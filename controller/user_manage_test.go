package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func setupManageUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled, previousSecret := common.RedisEnabled, common.SessionSecret
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	dialect := os.Getenv("TEST_MANAGE_USER_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	databaseTypes := map[string]common.DatabaseType{
		"sqlite": common.DatabaseTypeSQLite, "mysql": common.DatabaseTypeMySQL, "postgres": common.DatabaseTypePostgreSQL,
	}
	require.Contains(t, databaseTypes, dialect)
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	db, _ := newAuditTestDatabase(t, dialect, dsn)
	logDB := db
	if os.Getenv("TEST_MANAGE_USER_SEPARATE_LOG_DB") == "1" {
		logDB, _ = newAuditTestDatabase(t, dialect, dsn)
	}
	model.DB, model.LOG_DB = db, logDB
	common.RedisEnabled = false
	common.SessionSecret = "manage-user-test-secret"
	common.SetDatabaseTypes(databaseTypes[dialect], databaseTypes[dialect])

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.SessionSecret = previousRedisEnabled, previousSecret
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		if logDB != db {
			sqlLogDB, err := logDB.DB()
			if err == nil {
				_ = sqlLogDB.Close()
			}
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.CasbinRule{}, &model.AuthzRole{}, &model.AuthFlow{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.UserAccessToken{}, &model.Organization{}, &model.OrganizationMembership{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	versionQuery := "SELECT version()"
	if dialect == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s %s, separate log database: %v", dialect, version, logDB != db)
	return db
}

func performManageUserRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return performVerifiedManageUserRequest(t, body, service.AuthIdentity{UserID: 9999}, "")
}

// performVerifiedManageUserRequest drives ManageUser as the root operator from
// a dashboard login session, carrying the step-up proof status or role changes need.
func performVerifiedManageUserRequest(t *testing.T, body string, identity service.AuthIdentity, proof string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if proof != "" {
		c.Request.Header.Set("X-Security-Proof", proof)
	}
	c.Set("id", identity.UserID)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	c.Set("session_id", identity.SessionID)
	c.Set("auth_version", identity.UserAuthVersion)
	c.Set("session_version", identity.SessionVersion)
	c.Set(common.RequestIdKey, "quota-test-request")
	ManageUser(c)
	return recorder
}

// manageUserProof signs the root operator in and issues a password-method proof
// bound to one ManageUser operation.
func manageUserProof(t *testing.T, db *gorm.DB, operation service.VerificationOperation) (service.AuthIdentity, string) {
	t.Helper()
	operator := createQuotaTestOperator(t, db, common.RoleRootUser)
	require.NoError(t, model.PublishUserAuthCache(operator.Id))
	bundle, err := service.CreateLoginSession(operator.Id, "password", "127.0.0.1", "manage-user-test")
	require.NoError(t, err)
	identity, err := service.ParseAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	binding, err := service.BindVerificationOperation(operation)
	require.NoError(t, err)
	proof, _, err := service.IssueSecurityProof(identity, service.VerificationMethodPassword, binding)
	require.NoError(t, err)
	return identity, proof
}

func TestUserModelRateLimitAPI(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, scenario := range []string{"fresh", "upgrade"} {
				t.Run(scenario, func(t *testing.T) {
					db := setupUserModelRateLimitDB(t, dialect, scenario == "upgrade")
					verifyUserModelRateLimitAPI(t, db)
				})
			}
		})
	}
}

func setupUserManagementTestDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousRedis, previousSQLite := common.IsMasterNode, common.RedisEnabled, common.SQLitePath
	previousSecret := common.SessionSecret
	common.IsMasterNode, common.RedisEnabled = true, false
	common.SessionSecret = "user-model-rate-limit-test"
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.IsMasterNode, common.RedisEnabled, common.SQLitePath = previousMaster, previousRedis, previousSQLite
		common.SessionSecret = previousSecret
	})

	var base *gorm.DB
	var dsn string
	switch dialect {
	case "postgres":
		dsn = os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, parsed.Hostname() == "127.0.0.1" && parsed.Port() == "5432" && parsed.Path == "/newapi_e2e", "use only the local PostgreSQL E2E database")
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() { closeGormHandles(admin) })
		schema := fmt.Sprintf("user_rate_limit_test_%d", time.Now().UnixNano())
		name := clause.Table{Name: schema}
		require.NoError(t, admin.Exec("CREATE SCHEMA ?", name).Error)
		t.Cleanup(func() { assert.NoError(t, admin.Exec("DROP SCHEMA ? CASCADE", name).Error) })
		params := parsed.Query()
		params.Set("search_path", schema)
		parsed.RawQuery = params.Encode()
		dsn = parsed.String()
		base, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() { closeGormHandles(base) })
	case "mysql":
		dsn = os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		base, dsn = newAuditTestDatabase(t, dialect, dsn)
	case "sqlite":
		base, common.SQLitePath = newAuditTestDatabase(t, dialect, "")
		dsn = "local"
	}
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", "")
	versionSQL := "SELECT version()"
	if dialect == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, base.Raw(versionSQL).Scan(&version).Error)
	t.Logf("%s version: %s", dialect, version)
	return base
}

func setupUserModelRateLimitDB(t *testing.T, dialect string, upgrade bool) *gorm.DB {
	t.Helper()
	base := setupUserManagementTestDatabase(t, dialect)
	if upgrade {
		require.NoError(t, base.AutoMigrate(&releasedAuditUser{}))
		require.NoError(t, base.Create(&releasedAuditUser{Username: "released-user", AffCode: "released-aff"}).Error)
	}
	require.False(t, base.Migrator().HasTable(&model.UserModelRateLimit{}))
	require.NoError(t, model.InitDB())
	first := model.DB
	t.Cleanup(func() { closeGormHandles(first) })
	if upgrade {
		var existing model.User
		require.NoError(t, first.Where("username = ?", "released-user").Take(&existing).Error)
		assert.Equal(t, "released-aff", existing.AffCode)
		override, err := model.GetUserModelRateLimit(t.Context(), existing.Id)
		require.NoError(t, err)
		assert.Nil(t, override)
	}
	resident := model.User{Username: "rate-limit-resident", AffCode: "rate-limit-resident", Role: common.RoleCommonUser}
	require.NoError(t, first.Create(&resident).Error)
	require.NoError(t, resident.SetModelRateLimit(t.Context(), &model.UserModelRateLimit{Total: 0, Success: 7}))

	require.NoError(t, model.InitDB())
	db := model.DB
	t.Cleanup(func() { closeGormHandles(db) })
	require.NoError(t, model.InitLogDB())
	stored, err := model.GetUserModelRateLimit(t.Context(), resident.Id)
	require.NoError(t, err)
	assert.Equal(t, &model.UserModelRateLimit{UserID: resident.Id, Total: 0, Success: 7}, stored)
	require.Error(t, db.Create(&model.UserModelRateLimit{UserID: resident.Id}).Error, "a second row for a user must violate the primary key")
	return db
}

func TestUserListPersonalKeyUsage(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := setupUserManagementTestDatabase(t, dialect)
			logs := db
			// The SQLite case intentionally has no logs in the main database.
			if dialect == "sqlite" {
				logs, _ = newAuditTestDatabase(t, dialect, "")
			}
			model.DB, model.LOG_DB = db, logs
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			verifyUserListPersonalKeyUsage(t, db, logs, false)
		})
		t.Run(dialect+"_upgrade", func(t *testing.T) {
			db := setupUserManagementTestDatabase(t, dialect)
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			verifyUserListPersonalKeyUsage(t, db, db, true)
		})
	}
	if dsn := os.Getenv("TEST_CLICKHOUSE_LOG_DSN"); dsn != "" {
		t.Run("clickhouse_log", func(t *testing.T) {
			db := setupUserManagementTestDatabase(t, "sqlite")
			logs, logDSN := newAuditTestDatabase(t, "clickhouse", dsn)
			t.Setenv("LOG_SQL_DSN", logDSN)
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			require.NoError(t, model.InitLogDB())
			logDB := model.LOG_DB
			t.Cleanup(func() { closeGormHandles(logDB) })
			var version string
			require.NoError(t, logs.Raw("SELECT version()").Scan(&version).Error)
			t.Logf("ClickHouse version: %s", version)
			verifyUserListPersonalKeyUsage(t, db, model.LOG_DB, true)
		})
	}
}

func verifyUserListPersonalKeyUsage(t *testing.T, db, logs *gorm.DB, upgrade bool) {
	t.Helper()
	previousConsume, previousExport := common.LogConsumeEnabled, common.DataExportEnabled
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	t.Cleanup(func() { common.LogConsumeEnabled, common.DataExportEnabled = previousConsume, previousExport })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Task{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.Organization{}, &model.OrganizationMembership{}, &model.OrganizationMemberQuota{}, &model.OrganizationQuotaAccount{}))
	if upgrade {
		// Reconstruct the released schema before preparing the additive changes.
		require.NoError(t, db.Migrator().DropColumn(&model.User{}, "personal_key_used_quota"))
		require.NoError(t, db.Migrator().DropIndex(&model.Task{}, "RefundPendingAt"))
		require.NoError(t, db.Migrator().DropColumn(&model.Task{}, "refund_pending_at"))
		require.NoError(t, db.Migrator().DropColumn(&model.UserSubscription{}, "usage_reset_epoch"))
		require.NoError(t, db.Migrator().DropColumn(&model.SubscriptionPreConsumeRecord{}, "usage_reset_epoch"))
		require.Error(t, model.CheckBillingRollingSchema(db), "a new replica must not accept traffic against the old schema")
	}
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		require.NoError(t, logs.AutoMigrate(&model.Log{}))
	}
	users := []model.User{
		{Username: "key-usage-member", AffCode: "member", Quota: 500, UsedQuota: 900},
		{Username: "key-usage-none", AffCode: "none", UsedQuota: 700},
	}
	require.NoError(t, db.Omit("personal_key_used_quota").Create(&users).Error)
	member := users[0]
	org := model.Organization{Name: "Current organization", Status: model.OrganizationStatusActive}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, db.Create(&model.OrganizationMembership{OrganizationId: org.Id, UserId: member.Id, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive}).Error)
	require.NoError(t, db.Create(&model.OrganizationMemberQuota{OrganizationId: org.Id, UserId: member.Id, AllocatedQuota: 200, UsedQuota: 80}).Error)
	key := model.Token{UserId: member.Id, Key: "matrix-key", BillingOrganizationId: &org.Id}
	require.NoError(t, db.Create(&key).Error)
	require.NoError(t, db.Delete(&key).Error)

	wantUsage := 0
	if upgrade {
		require.NoError(t, logs.Create(&model.Log{
			UserId: member.Id, TokenId: key.Id, Type: model.LogTypeConsume, Quota: 100, Other: `{"billing_source":"wallet"}`,
		}).Error)
	}
	for range 2 {
		require.NoError(t, model.MigrateBillingRollingSchema(db, common.MainDatabaseType()))
	}
	require.NoError(t, model.CheckBillingRollingSchema(db))
	if upgrade {
		// An old node can update its original fields after the schema expansion.
		// The debit is real, but the personal Key counter does not include it.
		require.NoError(t, db.Model(&releasedAuditUser{}).Where("id = ?", member.Id).
			Updates(map[string]any{"quota": gorm.Expr("quota - ?", 10), "used_quota": gorm.Expr("used_quota + ?", 10)}).Error)
	}
	var migrated model.User
	require.NoError(t, db.First(&migrated, member.Id).Error)
	assert.Zero(t, migrated.PersonalKeyUsedQuota, "schema preparation must not import historical logs")
	if upgrade {
		assert.Equal(t, 490, migrated.Quota)
		assert.Equal(t, 910, migrated.UsedQuota)
	} else {
		assert.Equal(t, 500, migrated.Quota)
		assert.Equal(t, 900, migrated.UsedQuota)
	}
	assert.Error(t, db.Create(&model.User{Username: member.Username, AffCode: "duplicate-member"}).Error)

	common.LogConsumeEnabled = false
	sub := model.UserSubscription{UserId: member.Id, AmountTotal: 59, AmountUsed: 40}
	require.NoError(t, db.Create(&sub).Error)
	// Force a write failure after the receipt insert, then retry with the same identity.
	_, err := model.SettlePersonalKey("confirmed-subscription", member.Id, key.Id, 20, 20, sub.Id, nil)
	require.Error(t, err)
	var pending model.User
	require.NoError(t, db.First(&pending, member.Id).Error)
	assert.EqualValues(t, wantUsage, pending.PersonalKeyUsedQuota)
	require.NoError(t, db.Model(&sub).Update("amount_total", 60).Error)

	for _, tc := range []struct {
		id                  string
		subscription, delta int
	}{
		{"confirmed-wallet", 0, 20},
		{"confirmed-subscription", sub.Id, 20},
	} {
		applied, err := model.SettlePersonalKey(tc.id, member.Id, key.Id, 20, tc.delta, tc.subscription, nil)
		require.NoError(t, err)
		require.True(t, applied)
		applied, err = model.SettlePersonalKey(tc.id, member.Id, key.Id, 20, tc.delta, tc.subscription, nil)
		require.NoError(t, err)
		require.False(t, applied, "replaying the same settlement must not debit or count it again")
	}
	_, err = model.SettlePersonalKey("confirmed-wallet", member.Id, key.Id, 21, 21, 0, nil)
	require.Error(t, err)
	require.NoError(t, db.First(&sub, sub.Id).Error)
	assert.EqualValues(t, 60, sub.AmountUsed)

	wantUsage += 40
	task := model.Task{TaskID: "personal-key-usage-task", UserId: member.Id, Quota: 20, Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{TokenId: key.Id, BillingSource: service.BillingSourceWallet}}
	require.NoError(t, db.Create(&task).Error)
	oldTask := task
	applied, err := model.SettlePersonalKeyTask(&task, 35)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = model.SettlePersonalKeyTask(&oldTask, 35)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, db.First(&task, task.ID).Error)
	applied, err = model.SettlePersonalKeyTask(&task, 30)
	require.NoError(t, err)
	require.True(t, applied)
	wantUsage += 15

	// Even positive log rows are not settlements and must never increment the primary counter.
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.LogConsumeEnabled = true
	model.RecordConsumeLog(ctx, member.Id, model.RecordConsumeLogParams{TokenId: key.Id, Quota: 50})
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: member.Id, TokenId: key.Id, LogType: model.LogTypeConsume, Quota: 15})
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: member.Id, TokenId: key.Id, LogType: model.LogTypeRefund, Quota: 10})
	if upgrade {
		deleted, err := model.DeleteOldLogBatch(t.Context(), time.Now().Unix()+1, 100)
		require.NoError(t, err)
		assert.Positive(t, deleted)
	}
	require.NoError(t, model.MigrateBillingRollingSchema(db, common.MainDatabaseType()))
	model.LOG_DB = nil // The read path must not use the log store at all.

	engine := gin.New()
	engine.GET("/api/user/", GetAllUsers)
	engine.GET("/api/user/search", SearchUsers)
	for _, path := range []string{"/api/user/?sort_by=id&sort_order=asc", "/api/user/search?keyword=member"} {
		status, body := usageCall(t, engine, 0, common.RoleRootUser, path)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, true, body["success"], "%v", body)
		items := usageData(body)["items"].([]any)
		require.NotEmpty(t, items)
		row := items[0].(map[string]any)
		assert.EqualValues(t, wantUsage, row["personal_key_used_quota"], "only personally paid Key usage survives log cleanup")
		assert.EqualValues(t, migrated.Quota-30, row["quota"])
		assert.EqualValues(t, migrated.UsedQuota, row["used_quota"])
		assert.EqualValues(t, 80, row["organization"].(map[string]any)["used_quota"])
		if len(items) > 1 {
			assert.EqualValues(t, 0, items[1].(map[string]any)["personal_key_used_quota"])
		}
	}
}

func verifyUserModelRateLimitAPI(t *testing.T, db *gorm.DB) {
	t.Helper()
	previousSettings := setting.CurrentModelRequestRateLimit()
	setting.ModelRequestRateLimitMutex.Lock()
	setting.ModelRequestRateLimitEnabled = false
	setting.ModelRequestRateLimitDurationMinutes = 5
	setting.ModelRequestRateLimitCount = 23
	setting.ModelRequestRateLimitSuccessCount = 11
	setting.ModelRequestRateLimitMutex.Unlock()
	t.Cleanup(func() {
		setting.ModelRequestRateLimitMutex.Lock()
		setting.ModelRequestRateLimitEnabled = previousSettings.Enabled
		setting.ModelRequestRateLimitDurationMinutes = previousSettings.DurationMinutes
		setting.ModelRequestRateLimitCount = previousSettings.Total
		setting.ModelRequestRateLimitSuccessCount = previousSettings.Success
		setting.ModelRequestRateLimitMutex.Unlock()
	})

	users := []model.User{
		{Username: "rl-example-1", Group: "test", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "rl-example-2", Group: "test", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "rl-example-3", Group: "test", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "rl-example-4", Group: "other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "rl-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Username: "rl-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
	}
	for i := range users {
		users[i].AffCode = users[i].Username
		require.NoError(t, db.Create(&users[i]).Error)
	}
	owner, other, inherited, otherGroup, admin, root := users[0], users[1], users[2], users[3], users[4], users[5]
	org := model.Organization{Name: "Rate limit org", Status: model.OrganizationStatusActive}
	require.NoError(t, db.Create(&org).Error)
	for _, member := range []model.User{owner, other, inherited, otherGroup} {
		require.NoError(t, db.Create(&model.OrganizationMembership{UserId: member.Id, OrganizationId: org.Id, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive}).Error)
	}
	for _, target := range []model.User{other, otherGroup} {
		require.NoError(t, target.SetModelRateLimit(t.Context(), &model.UserModelRateLimit{Total: 5, Success: 6}))
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes := engine.Group("/api/user", middleware.AdminAuth())
	routes.GET("/search", SearchUsers)
	routes.GET("/:id/rate_limit", GetUserModelRateLimit)
	routes.PUT("/:id/rate_limit", UpdateUserModelRateLimit)
	_, adminJWT := createAccessTokenTestSession(t, admin.Id, "rate-limit-admin")
	_, rootJWT := createAccessTokenTestSession(t, root.Id, "rate-limit-root")
	_, ownerJWT := createAccessTokenTestSession(t, owner.Id, "rate-limit-owner")
	readToken, _ := createScopedAccessToken(t, admin.Id, 0, "user:read")
	writeToken, _ := createScopedAccessToken(t, admin.Id, 0, "user:write")

	type envelope struct {
		Success bool              `json:"success"`
		Data    common.RawMessage `json:"data"`
	}
	limiterPath := func(id int) string { return fmt.Sprintf("/api/user/%d/rate_limit", id) }
	call := func(method, path, credential, body string, status int, success bool) common.RawMessage {
		t.Helper()
		response := accessTokenRequest(engine, method, path, credential, "", body)
		require.Equal(t, status, response.Code, response.Body.String())
		var result envelope
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, success, result.Success, response.Body.String())
		return result.Data
	}
	const limits = `{"override":{"total":0,"success":2147483647}}`
	const remove = `{"override":null}`
	const withDefaults = `{"enabled":false,"duration_minutes":5,"defaults":{"total":23,"success":11},"override":`
	assert.JSONEq(t, withDefaults+`null}`, string(call(http.MethodGet, limiterPath(owner.Id), readToken, "", http.StatusOK, true)))
	call(http.MethodGet, limiterPath(owner.Id), writeToken, "", http.StatusForbidden, false)
	call(http.MethodPut, limiterPath(owner.Id), readToken, limits, http.StatusForbidden, false)
	call(http.MethodPut, limiterPath(owner.Id), ownerJWT, limits, http.StatusForbidden, false)
	for _, target := range []model.User{admin, root} {
		call(http.MethodGet, limiterPath(target.Id), adminJWT, "", http.StatusOK, false)
		call(http.MethodPut, limiterPath(target.Id), adminJWT, limits, http.StatusOK, false)
	}
	call(http.MethodGet, limiterPath(admin.Id), rootJWT, "", http.StatusOK, true)
	call(http.MethodPut, limiterPath(admin.Id), rootJWT, limits, http.StatusOK, true)
	call(http.MethodPut, limiterPath(99999999), rootJWT, limits, http.StatusOK, false)

	assert.JSONEq(t, withDefaults+`{"total":0,"success":2147483647}}`, string(call(http.MethodPut, limiterPath(owner.Id), writeToken, limits, http.StatusOK, true)))
	for _, invalid := range []string{
		`{}`, `null`, `{"override":{}}`, `{"override":{"total":0}}`, `{"override":{"success":0}}`,
		`{"override":{"total":null,"success":0}}`, `{"override":{"total":0,"success":null}}`,
		`{"override":{"total":-1,"success":0}}`, `{"override":{"total":0,"success":-1}}`,
		`{"override":{"total":0,"success":2147483648}}`, `{"override":{"total":1.5,"success":0}}`,
		`{"override":{"total":"1","success":0}}`, `{"override":false}`, `{"override":null}{}`,
	} {
		call(http.MethodPut, limiterPath(owner.Id), adminJWT, invalid, http.StatusOK, false)
	}
	assert.JSONEq(t, withDefaults+`{"total":0,"success":2147483647}}`, string(call(http.MethodGet, limiterPath(owner.Id), adminJWT, "", http.StatusOK, true)))
	call(http.MethodPut, limiterPath(owner.Id), adminJWT, `{"override":{"total":0,"success":0}}`, http.StatusOK, true)
	assert.JSONEq(t, withDefaults+`{"total":0,"success":0}}`, string(call(http.MethodGet, limiterPath(owner.Id), adminJWT, "", http.StatusOK, true)))

	search := fmt.Sprintf("/api/user/search?keyword=rl-example&group=test&role=%d&status=%d&organization_id=%d&sort_by=id&sort_order=asc&page_size=1", common.RoleCommonUser, common.UserStatusEnabled, org.Id)
	for _, page := range []struct{ number, total, id int }{{1, 2, owner.Id}, {2, 2, other.Id}, {3, 2, 0}} {
		var data struct {
			Total int          `json:"total"`
			Items []model.User `json:"items"`
		}
		require.NoError(t, common.Unmarshal(call(http.MethodGet, fmt.Sprintf("%s&has_rate_limit_override=true&p=%d", search, page.number), adminJWT, "", http.StatusOK, true), &data))
		assert.Equal(t, page.total, data.Total)
		if page.id == 0 {
			assert.Empty(t, data.Items)
		} else {
			require.Len(t, data.Items, 1)
			assert.Equal(t, page.id, data.Items[0].Id)
		}
	}
	call(http.MethodPut, limiterPath(owner.Id), adminJWT, remove, http.StatusOK, true)
	call(http.MethodPut, limiterPath(owner.Id), adminJWT, remove, http.StatusOK, true)
	assert.JSONEq(t, withDefaults+`null}`, string(call(http.MethodGet, limiterPath(owner.Id), adminJWT, "", http.StatusOK, true)))
	assert.Contains(t, string(call(http.MethodGet, search+"&has_rate_limit_override=true", adminJWT, "", http.StatusOK, true)), `"total":1`)
	assert.Contains(t, string(call(http.MethodGet, search+"&has_rate_limit_override=false", adminJWT, "", http.StatusOK, true)), `"total":3`)

	// Cleanup also covers the hard-delete path; a removed user cannot leave a reusable override.
	require.NoError(t, otherGroup.SetModelRateLimit(t.Context(), &model.UserModelRateLimit{Total: 1}))
	_, err := model.HardDeleteUserById(otherGroup.Id)
	require.NoError(t, err)
	stored, err := model.GetUserModelRateLimit(t.Context(), otherGroup.Id)
	require.NoError(t, err)
	assert.Nil(t, stored)
}

func TestManageUserDisableAdvancesAuthVersionOnceAndRevokesSession(t *testing.T) {
	db := setupManageUserTestDB(t)
	now := time.Now().Unix()
	user := model.User{
		Username: "managed-disable-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.UserSession{
		SID: "managed-disable-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: model.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	identity, proof := manageUserProof(t, db, service.VerificationOperation{Scope: service.VerificationScopeAdminUserManage, Context: []byte(fmt.Sprintf(`{"user_id":%d,"action":"disable"}`, user.Id))})
	recorder := performVerifiedManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"disable"}`, user.Id), identity, proof)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, updated.Status)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var session model.UserSession
	require.NoError(t, db.First(&session, "sid = ?", "managed-disable-session").Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
}

func TestManageUserDemoteAdvancesAuthVersionAndRevokesSessionsOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))

	now := time.Now().Unix()
	user := model.User{
		Username: "managed-demote-user", Password: "password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	for _, sid := range []string{"managed-demote-session-one", "managed-demote-session-two"} {
		require.NoError(t, db.Create(&model.UserSession{
			SID: sid, UserID: user.Id, Version: 1, UserAuthVersion: 1,
			Status: model.UserSessionStatusActive, RefreshHash: "refresh-" + sid, LoginMethod: "password",
			LastActiveAt: now, ExpiresAt: now + 3600,
		}).Error)
	}

	sessionUpdateCount := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:count_demote_session_updates", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "user_sessions" {
			sessionUpdateCount++
		}
	}))

	identity, proof := manageUserProof(t, db, service.VerificationOperation{Scope: service.VerificationScopeAdminUserManage, Context: []byte(fmt.Sprintf(`{"user_id":%d,"action":"demote"}`, user.Id))})
	recorder := performVerifiedManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"demote"}`, user.Id), identity, proof)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.RoleCommonUser, updated.Role)
	assert.EqualValues(t, 2, updated.AuthVersion)
	var sessions []model.UserSession
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("sid asc").Find(&sessions).Error)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
		assert.Equal(t, "admin_demote", session.RevokedReason)
	}
	assert.Equal(t, 1, sessionUpdateCount)
}

func TestManageUserDeleteReturnsImmediatelyAndUnknownActionFails(t *testing.T) {
	db := setupManageUserTestDB(t)
	deleted := model.User{
		Username: "managed-delete-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "delete-aff",
	}
	require.NoError(t, db.Create(&deleted).Error)

	identity, proof := manageUserProof(t, db, service.VerificationOperation{Scope: service.VerificationScopeAdminUserDelete, Context: []byte(fmt.Sprintf(`{"user_id":%d}`, deleted.Id))})
	recorder := performVerifiedManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"delete"}`, deleted.Id), identity, proof)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	var deletedCount int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("id = ? AND deleted_at IS NOT NULL", deleted.Id).Count(&deletedCount).Error)
	assert.EqualValues(t, 1, deletedCount)

	unchanged := model.User{
		Username: "managed-unknown-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: "unknown-aff",
	}
	require.NoError(t, db.Create(&unchanged).Error)
	recorder = performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"unknown"}`, unchanged.Id))
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	require.NoError(t, db.First(&unchanged, unchanged.Id).Error)
	assert.EqualValues(t, 1, unchanged.AuthVersion)
	assert.Equal(t, common.UserStatusEnabled, unchanged.Status)
}

func createQuotaTestOperator(t *testing.T, db *gorm.DB, role int) model.User {
	t.Helper()
	if role == 0 {
		role = common.RoleRootUser
	}
	operator := model.User{Id: 9999, Username: "root-operator", Password: "root-operator-hash", Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "root-operator-aff"}
	require.NoError(t, db.Create(&operator).Error)
	return operator
}

func TestManageUserQuotaRecordsTopupAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name, mode, action, content string
		value, wantQuota            int
	}{
		{"add", "add", "user.quota_add", "Increased user quota by 500", 500, 1500},
		{"subtract", "subtract", "user.quota_subtract", "Decreased user quota by 500", 500, 500},
		{"override_up", "override", "user.quota_override", "Overrode user quota from 1000 to 1500", 1500, 1500},
		{"override_down", "override", "user.quota_override", "Overrode user quota from 1000 to 500", 500, 500},
		{"override_unchanged", "override", "user.quota_override", "Overrode user quota from 1000 to 1000", 1000, 1000},
		{"override_zero", "override", "user.quota_override", "Overrode user quota from 1000 to 0", 0, 0},
		{"override_negative", "override", "user.quota_override", "Overrode user quota from 1000 to -1", -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000, AffCode: "quota-owner-aff"}
			require.NoError(t, db.Create(&user).Error)
			createQuotaTestOperator(t, db, common.RoleRootUser)

			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"success":true`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.wantQuota, user.Quota)

			logs, total, err := model.GetAllLogs(model.LogFilter{Type: model.LogTypeTopup}, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, logs, 1)
			assert.Equal(t, user.Id, logs[0].UserId)
			assert.Equal(t, user.Username, logs[0].Username)
			assert.Equal(t, tc.content, logs[0].Content)
			model.FormatAdminLogs(logs)
			var other model.AuditOther
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Equal(t, &model.AuditAdminInfo{AdminID: 9999, AdminUsername: "root-operator", AdminRole: common.RoleRootUser, AuthMethod: "session"}, other.AdminInfo)

			logs, total, err = model.GetUserLogs(user.Id, model.LogFilter{Type: model.LogTypeTopup}, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, logs, 1)
			other = model.AuditOther{}
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Nil(t, other.AdminInfo)
			require.NotNil(t, other.Op)
			assert.Equal(t, tc.action, other.Op.Action)
			params, err := common.Marshal(other.Op.Params)
			require.NoError(t, err)
			expectedParams := model.AuditFields{"target_user_id": user.Id, "target_username": user.Username, "mode": tc.mode, "requested_quota": tc.value, "from": 1000, "to": tc.wantQuota}
			if tc.mode != "override" {
				expectedParams["quota"] = tc.value
			}
			expected, err := common.Marshal(expectedParams)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.Equal(t, "quota-test-request", logs[0].RequestId)
			assert.Empty(t, logs[0].Ip, "recipient logs must not disclose the administrator IP")

			logs, total, err = model.GetUserLogs(9999, model.LogFilter{Type: model.LogTypeTopup}, 0, 20)
			require.NoError(t, err)
			assert.Zero(t, total)
			assert.Empty(t, logs)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			require.Len(t, audits, 1)
			assert.Equal(t, 9999, audits[0].UserId)
			assert.Equal(t, "root-operator", audits[0].Username)
			assert.Equal(t, tc.action, audits[0].Action)
			assert.True(t, audits[0].Success)
			params, err = common.Marshal(audits[0].Other.Op.Params)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.Equal(t, "quota-test-request", audits[0].RequestId)
		})
	}
}

func TestManageUserQuotaFailuresDoNotRecordTopup(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		value      int
		failUpdate bool
	}{
		{"zero_add", "add", 0, false},
		{"negative_subtract", "subtract", -1, false},
		{"invalid_mode", "invalid", 500, false},
		{"add_update_error", "add", 500, true},
		{"subtract_update_error", "subtract", 500, true},
		{"override_update_error", "override", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, common.RoleRootUser)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000}
			require.NoError(t, db.Create(&user).Error)
			if tc.failUpdate {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_quota_update", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("quota update unavailable"))
					}
				}))
			}
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Contains(t, recorder.Body.String(), `"success":false`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 1000, user.Quota)
			var logCount, auditCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logCount).Error)
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&auditCount).Error)
			assert.Zero(t, logCount)
			assert.EqualValues(t, 1, auditCount)
			var audit model.AuditLog
			require.NoError(t, model.LOG_DB.First(&audit).Error)
			assert.False(t, audit.Success)
			params, err := common.Marshal(audit.Other.Op.Params)
			require.NoError(t, err)
			assert.NotContains(t, string(params), `"from"`)
			assert.NotContains(t, string(params), `"to"`)
			assert.NotContains(t, string(params), `"target_username"`)
			assert.NotContains(t, string(params), "quota update unavailable")
			reason := "invalid_parameters"
			if tc.failUpdate {
				reason = "database_error"
			}
			assert.Contains(t, string(params), `"failure_reason":"`+reason+`"`)
			assert.Contains(t, string(params), fmt.Sprintf(`"requested_quota":%d`, tc.value))
		})
	}
}

func TestManageUserQuotaLogFailureKeepsSuccessfulAdjustment(t *testing.T) {
	for _, failedTable := range []string{"logs", "audit_logs"} {
		t.Run(failedTable, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, common.RoleRootUser)
			user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("test:fail_quota_log", func(tx *gorm.DB) {
				if tx.Statement.Table == failedTable {
					tx.AddError(errors.New("quota log unavailable"))
				}
			}))
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":"add","value":500}`, user.Id))
			assert.Contains(t, recorder.Body.String(), `"success":true`)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 1500, user.Quota)
			var logCount, auditCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logCount).Error)
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&auditCount).Error)
			if failedTable == "logs" {
				assert.Zero(t, logCount)
				assert.EqualValues(t, 1, auditCount)
			} else {
				assert.EqualValues(t, 1, logCount)
				assert.Zero(t, auditCount)
			}
		})
	}
}

func TestManageUserQuotaTargetsAndWalletBounds(t *testing.T) {
	for _, tc := range []struct {
		name, mode, reason                                string
		before, value, targetID, targetRole, operatorRole int
		deleted, failRead                                 bool
	}{
		{name: "zero_id", mode: "add", value: 1, reason: "invalid_parameters"},
		{name: "negative_id", mode: "subtract", value: 1, targetID: -1, reason: "invalid_parameters"},
		{name: "missing", mode: "override", value: 1, targetID: 12345, reason: "target_not_found"},
		{name: "deleted", mode: "add", value: 1, targetID: 1, deleted: true, reason: "target_not_found"},
		{name: "peer_admin", mode: "add", value: 1, targetID: 1, targetRole: common.RoleAdminUser, operatorRole: common.RoleAdminUser, reason: "permission_denied"},
		{name: "higher_role", mode: "override", value: 1, targetID: 1, targetRole: common.RoleRootUser, operatorRole: common.RoleAdminUser, reason: "permission_denied"},
		{name: "read_error", mode: "add", value: 1, targetID: 1, failRead: true, reason: "database_error"},
		{name: "add_overflow", mode: "add", before: common.MaxWalletQuota, value: 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "subtract_underflow", mode: "subtract", before: -common.MaxWalletQuota, value: 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_add", mode: "add", value: common.MaxWalletQuota + 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_subtract", mode: "subtract", value: common.MaxWalletQuota + 1, targetID: 1, reason: "quota_limit_exceeded"},
		{name: "oversized_override", mode: "override", value: -common.MaxWalletQuota - 1, targetID: 1, reason: "quota_limit_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			createQuotaTestOperator(t, db, tc.operatorRole)
			user := model.User{Id: 1, Username: "private-target-name", Role: tc.targetRole, Quota: tc.before}
			require.NoError(t, db.Create(&user).Error)
			if tc.deleted {
				require.NoError(t, db.Delete(&user).Error)
			}
			if tc.failRead {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:quota_read_error", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" && len(tx.Statement.Selects) == 0 {
						tx.AddError(errors.New("private database error"))
					}
				}))
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, tc.targetID, tc.mode, tc.value)))
			role := tc.operatorRole
			if role == 0 {
				role = common.RoleRootUser
			}
			c.Set("id", 9999)
			c.Set("role", role)
			ManageUser(c)
			require.Contains(t, recorder.Body.String(), `"success":false`)
			if tc.failRead {
				require.NoError(t, db.Callback().Query().Remove("test:quota_read_error"))
			}
			require.NoError(t, db.Unscoped().First(&user, user.Id).Error)
			assert.Equal(t, tc.before, user.Quota)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Find(&audits).Error)
			require.Len(t, audits, 1)
			assert.False(t, audits[0].Success)
			params, err := common.Marshal(audits[0].Other.Op.Params)
			require.NoError(t, err)
			expected, err := common.Marshal(model.AuditFields{"target_user_id": tc.targetID, "mode": tc.mode, "requested_quota": tc.value, "failure_reason": tc.reason})
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(params))
			assert.NotContains(t, audits[0].Content, user.Username)
			var count int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestManageUserQuotaMiddlewareKeepsOneOperationPerRequest(t *testing.T) {
	db := setupManageUserTestDB(t)
	pat := "quota-middleware-test-token"
	operator := model.User{Id: 9999, Username: "root-operator", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &pat, Quota: 1000}
	require.NoError(t, db.Create(&operator).Error)
	router := gin.New()
	router.Use(middleware.RequestId(), middleware.AccessTokenAudit())
	router.POST("/api/user/manage", middleware.AdminAuth(), ManageUser)
	for _, tc := range []struct {
		body, action string
		success      bool
	}{
		{`{"id":9999,"action":"add_quota","mode":"add","value":100}`, "user.quota_add", true},
		{`{"id":9999,"action":"add_quota","mode":"subtract","value":0}`, "user.quota_subtract", false},
		{`{"id":9999,"action":"add_quota","mode":"invalid","value":1}`, "generic", false},
		{`{"id":`, "generic", false},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+pat)
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`"success":%t`, tc.success))
		requestID := recorder.Header().Get(common.RequestIdKey)
		require.NotEmpty(t, requestID)
		var audits []model.AuditLog
		require.NoError(t, model.LOG_DB.Where("request_id = ?", requestID).Find(&audits).Error)
		require.Len(t, audits, 2, "one operation audit and one PAT request audit")
		for _, audit := range audits {
			assert.Equal(t, tc.success, audit.Success)
			if audit.Category != model.AuditCategoryOperation {
				continue
			}
			assert.Equal(t, tc.action, audit.Action)
			assert.Equal(t, operator.Id, audit.UserId)
			assert.Equal(t, "/api/user/manage", audit.Route)
			if tc.success {
				params, err := common.Marshal(audit.Other.Op.Params)
				require.NoError(t, err)
				assert.Contains(t, string(params), `"target_user_id":9999`)
				assert.Contains(t, string(params), `"target_username":"root-operator"`)
			}
		}
		var count int64
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", requestID).Count(&count).Error)
		if tc.success {
			assert.EqualValues(t, 1, count)
		} else {
			assert.Zero(t, count)
		}
	}
	require.NoError(t, db.First(&operator, operator.Id).Error)
	assert.Equal(t, 1100, operator.Quota)
}

func TestManageUserQuotaConcurrentSnapshots(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{Username: "concurrent-quota", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	var ready sync.WaitGroup
	ready.Add(2)
	release := make(chan struct{})
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:concurrent_quota_start", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			ready.Done()
			<-release
		}
	}))
	type result struct {
		adjustment *model.UserQuotaAdjustment
		err        error
		value      int
	}
	results := make(chan result, 2)
	for _, value := range []int{10, 20} {
		go func(value int) {
			adjustment, err := model.AdjustUserQuota(user.Id, common.RoleRootUser, "add", value)
			results <- result{adjustment, err, value}
		}(value)
	}
	ready.Wait()
	close(release)
	var committed []model.UserQuotaAdjustment
	for range 2 {
		result := <-results
		if result.err != nil {
			require.True(t, common.UsingMainDatabase(common.DatabaseTypeSQLite), "row-locking databases must serialize both adjustments: %v", result.err)
			assert.Contains(t, strings.ToLower(result.err.Error()), "locked")
			assert.Nil(t, result.adjustment)
			continue
		}
		require.NotNil(t, result.adjustment)
		assert.Equal(t, result.value, result.adjustment.After-result.adjustment.Before)
		committed = append(committed, *result.adjustment)
	}
	require.NoError(t, db.Callback().Query().Remove("test:concurrent_quota_start"))
	require.NotEmpty(t, committed)
	sort.Slice(committed, func(i, j int) bool { return committed[i].Before < committed[j].Before })
	balance := 1000
	for _, adjustment := range committed {
		assert.Equal(t, balance, adjustment.Before)
		balance = adjustment.After
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, balance, user.Quota)
}

func TestManageUserQuotaCacheUsesCommittedIntegerDifference(t *testing.T) {
	for _, tc := range []struct {
		name, mode                               string
		before, cached, value, after, wantCached int
		failUpdate, failCache, missingCache      bool
	}{
		{name: "add_preserves_reservations", mode: "add", before: 1000, cached: 900, value: 500, after: 1500, wantCached: 1400},
		{name: "subtract_preserves_reservations", mode: "subtract", before: 1000, cached: 900, value: 500, after: 500, wantCached: 400},
		{name: "override_preserves_reservations", mode: "override", before: 1000, cached: 900, value: 2000, after: 2000, wantCached: 1900},
		{name: "large_odd_difference", mode: "override", before: common.MaxWalletQuota - 1, cached: common.MaxWalletQuota - 1, value: -common.MaxWalletQuota, after: -common.MaxWalletQuota, wantCached: -common.MaxWalletQuota},
		{name: "rollback_does_not_change_cache", mode: "subtract", before: 1000, cached: 900, value: 500, after: 1000, wantCached: 900, failUpdate: true},
		{name: "cache_error_keeps_committed_change", mode: "add", before: 1000, value: 500, after: 1500, failCache: true},
		{name: "missing_cache_is_not_partially_created", mode: "add", before: 1000, value: 500, after: 1500, missingCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			operator := createQuotaTestOperator(t, db, common.RoleRootUser)
			server := miniredis.RunT(t)
			oldRDB := common.RDB
			common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RedisEnabled = true
			t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = oldRDB })
			user := model.User{Username: "cached-quota", Quota: tc.before, AuthVersion: 1}
			require.NoError(t, db.Create(&user).Error)
			cache, err := model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, tc.before, cache.Quota)
			_, err = model.GetUserCache(operator.Id)
			require.NoError(t, err)
			keys := server.Keys()
			var quotaKey string
			for _, key := range keys {
				if server.HGet(key, "Id") == strconv.Itoa(user.Id) && server.HGet(key, "Quota") != "" {
					quotaKey = key
					break
				}
			}
			require.NotEmpty(t, quotaKey)
			server.HSet(quotaKey, "Quota", strconv.Itoa(tc.cached))
			if tc.missingCache {
				server.Del(quotaKey)
			}
			if tc.failCache {
				server.SetError("ERR quota cache unavailable")
			}
			if tc.failUpdate {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:cache_quota_rollback", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("quota rollback"))
					}
				}))
			}
			recorder := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","mode":%q,"value":%d}`, user.Id, tc.mode, tc.value))
			assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`"success":%t`, !tc.failUpdate))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.after, user.Quota)
			if tc.missingCache {
				// Log username lookup may hydrate the whole user after commit.
				if server.Exists(quotaKey) {
					assert.Equal(t, strconv.Itoa(user.Id), server.HGet(quotaKey, "Id"))
					assert.NotEmpty(t, server.HGet(quotaKey, "CacheSchema"))
					assert.Equal(t, strconv.Itoa(tc.after), server.HGet(quotaKey, "Quota"))
				}
			} else if !tc.failCache {
				assert.Equal(t, strconv.Itoa(tc.wantCached), server.HGet(quotaKey, "Quota"))
			}
		})
	}
}

// Saving notification settings starts from what the user stored: fields the
// form does not manage stay, and switches an older client leaves out keep
// their stored values. Only administrators change the channel and upstream
// switches, and a threshold of 0 means the site default.
func TestUpdateUserSettingKeepsWhatTheRequestLeavesOut(t *testing.T) {
	on, off := true, false
	stored := dto.UserSetting{
		NotifyType:                       dto.NotifyTypeBark,
		BarkUrl:                          "https://api.day.app/key/{{title}}/{{content}}",
		QuotaWarningThreshold:            500000,
		QuotaWarningEnabled:              &off,
		ChannelNotifyEnabled:             &on,
		UpstreamModelUpdateNotifyEnabled: true,
		Language:                         "zhCN",
		SidebarModules:                   `{"chat":{"enabled":true}}`,
		BillingPreference:                "wallet_only",
	}
	kept := func(setting dto.UserSetting) dto.UserSetting {
		setting.Language, setting.SidebarModules, setting.BillingPreference = stored.Language, stored.SidebarModules, stored.BillingPreference
		return setting
	}
	switchesChanged := `{"notify_type":"email","quota_warning_threshold":0,"quota_warning_enabled":true,"channel_notify_enabled":false,"upstream_model_update_notify_enabled":false}`
	for _, tc := range []struct {
		name        string
		role        int
		body        string
		wantSuccess bool
		want        dto.UserSetting
	}{
		{
			name:        "older client without the new switches",
			role:        common.RoleAdminUser,
			body:        `{"notify_type":"webhook","quota_warning_threshold":2000,"webhook_url":"https://example.com/hook","webhook_secret":"hook-secret","accept_unset_model_ratio_model":true,"record_ip_log":true}`,
			wantSuccess: true,
			want: kept(dto.UserSetting{
				NotifyType: dto.NotifyTypeWebhook, WebhookUrl: "https://example.com/hook", WebhookSecret: "hook-secret",
				QuotaWarningThreshold: 2000, QuotaWarningEnabled: &off, ChannelNotifyEnabled: &on, UpstreamModelUpdateNotifyEnabled: true,
				AcceptUnsetRatioModel: true, RecordIpLog: true,
			}),
		},
		{
			name:        "administrator changes the switches and uses the site default threshold",
			role:        common.RoleAdminUser,
			body:        switchesChanged,
			wantSuccess: true,
			want:        kept(dto.UserSetting{NotifyType: dto.NotifyTypeEmail, QuotaWarningEnabled: &on, ChannelNotifyEnabled: &off}),
		},
		{
			name:        "user cannot change the administrator switches",
			role:        common.RoleCommonUser,
			body:        switchesChanged,
			wantSuccess: true,
			want:        kept(dto.UserSetting{NotifyType: dto.NotifyTypeEmail, QuotaWarningEnabled: &on, ChannelNotifyEnabled: &on, UpstreamModelUpdateNotifyEnabled: true}),
		},
		{
			name: "negative threshold",
			role: common.RoleCommonUser,
			body: `{"notify_type":"email","quota_warning_threshold":-1}`,
			want: stored,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			setting, err := common.Marshal(stored)
			require.NoError(t, err)
			user := model.User{Username: "setting-owner", Role: tc.role, Status: common.UserStatusEnabled, AffCode: "setting-owner-aff", Setting: string(setting)}
			require.NoError(t, db.Create(&user).Error)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("id", user.Id)
			UpdateUserSetting(c)

			assert.Contains(t, recorder.Body.String(), fmt.Sprintf(`"success":%t`, tc.wantSuccess))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.want, user.GetSetting())
		})
	}
}
