package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Organization tests run on SQLite by default. Set TEST_ORGANIZATION_DIALECT
// (sqlite, mysql, postgres) with TEST_<DIALECT>_DSN to run the same contract on
// another database; see newAuditTestDatabase for how isolated databases are made.
func setupOrganizationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dialect := os.Getenv("TEST_ORGANIZATION_DIALECT")
	if dialect == "" {
		dialect = "sqlite"
	}
	databaseTypes := map[string]common.DatabaseType{
		"sqlite": common.DatabaseTypeSQLite, "mysql": common.DatabaseTypeMySQL, "postgres": common.DatabaseTypePostgreSQL,
	}
	require.Contains(t, databaseTypes, dialect)
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	var db *gorm.DB
	if dialect == "sqlite" {
		// Use the production SQLite settings so concurrent writers wait for the
		// lock instead of failing immediately with SQLITE_BUSY.
		var err error
		db, err = gorm.Open(sqlite.Open(t.TempDir()+"/organization.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
		require.NoError(t, err)
	} else {
		db, _ = newAuditTestDatabase(t, dialect, dsn)
	}

	previousDB := model.DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	model.DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(databaseTypes[dialect], databaseTypes[dialect])
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.AuthFlow{},
		&model.PasskeyCredential{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.Token{}, &model.ExternalIdentityClaim{},
		&model.UserOAuthBinding{}, &model.Token{}, &model.Organization{}, &model.OrganizationMembership{}, &model.OrganizationQuotaAccount{},
		&model.OrganizationMemberQuota{}, &model.OrganizationQuotaLedgerEntry{}))
	return db
}

// createOrganizationTestUser creates an ordinary account that belongs to no
// organization and returns its id.
func createOrganizationTestUser(t *testing.T, db *gorm.DB, username string, status int) int {
	t.Helper()
	user := &model.User{Username: username, Password: "not-a-real-password", Role: common.RoleCommonUser, Status: status, AffCode: "aff-" + username}
	require.NoError(t, db.Create(user).Error)
	return user.Id
}

// organizationTestEngine serves the organization routes and the self password
// change route as the given user. The auth middleware is replaced by setting the
// context keys it sets, so these tests cover the organization contract itself.
func organizationTestEngine(userId int, role int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", userId)
		c.Set("role", role)
		c.Next()
	})
	root := engine.Group("/api/organization")
	root.GET("/", GetOrganizations)
	root.POST("/", CreateOrganization)
	root.GET("/:id", GetOrganization)
	root.PUT("/:id", UpdateOrganization)
	root.PUT("/:id/status", UpdateOrganizationStatus)
	root.GET("/:id/quota", RootGetOrganizationQuota)
	root.POST("/:id/quota/grant", RootGrantOrganizationQuota)
	root.POST("/:id/quota/adjust", RootAdjustOrganizationQuota)
	root.PUT("/:id/members/:user_id/quota", RootSetOrganizationMemberAllocation)
	root.POST("/:id/requests/:request_id/refund", RootRefundOrganizationRequest)
	root.GET("/:id/deletion-impact", GetOrganizationDeletionImpact)
	root.DELETE("/:id", DeleteOrganization)

	self := engine.Group("/api/organization/self")
	self.GET("", GetOrganizationSelf)
	self.GET("/members", ListOrganizationMembers)
	self.GET("/quota", GetOwnOrganizationQuota)
	self.POST("/members", CreateOrganizationMember)
	self.PUT("/members/:user_id", UpdateOrganizationMember)
	self.PUT("/members/:user_id/quota", SetOwnOrganizationMemberAllocation)
	self.DELETE("/members/:user_id", DeleteOrganizationMember)
	self.POST("/members/:user_id/password", ResetOrganizationMemberPassword)

	engine.PUT("/api/user/self/password", ChangeOwnPassword)
	return engine
}

const (
	organizationTestRootId = 1
	organizationTestPass   = "initial-pass-1"
)

func doOrganizationRequest(t *testing.T, userId int, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	role := common.RoleCommonUser
	if userId == organizationTestRootId {
		role = common.RoleRootUser
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	organizationTestEngine(userId, role).ServeHTTP(recorder, req)
	return recorder
}

// organizationResponseData decodes the data field of a success response, or
// returns the HTTP status for a failure.
func organizationResponseData(t *testing.T, recorder *httptest.ResponseRecorder) (json.RawMessage, int) {
	t.Helper()
	var body struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body.Data, recorder.Code
}

func organizationErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body.Code
}

// createOrganizationWithAdmin creates an organization through the Root route and
// returns its id. The admin account is created with organizationTestPass.
func createOrganizationWithAdmin(t *testing.T, name string, adminUsername string) int {
	t.Helper()
	recorder := doOrganizationRequest(t, organizationTestRootId, http.MethodPost, "/api/organization/",
		fmt.Sprintf(`{"name":%q,"admin_username":%q,"admin_password":%q}`, name, adminUsername, organizationTestPass))
	data, status := organizationResponseData(t, recorder)
	require.Equal(t, http.StatusOK, status, recorder.Body.String())
	var organization model.Organization
	require.NoError(t, common.Unmarshal(data, &organization))
	require.NotZero(t, organization.Id)
	return organization.Id
}

// userByName loads an account created through the organization routes.
func userByName(t *testing.T, db *gorm.DB, username string) model.User {
	t.Helper()
	var user model.User
	require.NoError(t, db.Where("username = ?", username).First(&user).Error)
	return user
}

// Quota limits: organization admins set ordinary members' allocations only, and Root
// sets any allocation. Forbidden writes return a stable code and change nothing.
func TestOrganizationQuotaAllocationPermissions(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	orgId := createOrganizationWithAdmin(t, "Quota Org", "quota-admin")
	adminId := userByName(t, db, "quota-admin").Id

	recorder := doOrganizationRequest(t, adminId, http.MethodPost, "/api/organization/self/members",
		`{"username":"quota-member","password":"`+organizationTestPass+`"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	memberId := userByName(t, db, "quota-member").Id

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPost,
		fmt.Sprintf("/api/organization/%d/quota/grant", orgId), `{"amount":1000,"reason":"initial","reference":"grant-1"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = doOrganizationRequest(t, adminId, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d/quota", memberId), `{"allocated":300}`)
	assert.Equal(t, http.StatusOK, recorder.Code, "an admin may set an ordinary member's allocation")

	recorder = doOrganizationRequest(t, adminId, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d/quota", adminId), `{"allocated":300}`)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Equal(t, "ORGANIZATION_PERMISSION_DENIED", organizationErrorCode(t, recorder), "an admin cannot set their own allocation")

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPut,
		fmt.Sprintf("/api/organization/%d/members/%d/quota", orgId, adminId), `{"allocated":200}`)
	assert.Equal(t, http.StatusOK, recorder.Code, "Root may set an admin's allocation")

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodGet,
		fmt.Sprintf("/api/organization/%d/quota", orgId), "")
	data, status := organizationResponseData(t, recorder)
	require.Equal(t, http.StatusOK, status, recorder.Body.String())
	var reconciliation struct {
		GrantedQuota int64 `json:"granted_quota"`
		Allocated    int64 `json:"allocated"`
		Balanced     bool  `json:"balanced"`
	}
	require.NoError(t, common.Unmarshal(data, &reconciliation))
	assert.Equal(t, int64(1000), reconciliation.GrantedQuota)
	assert.Equal(t, int64(500), reconciliation.Allocated)
	assert.True(t, reconciliation.Balanced)
}

// The self-user payload carries the active membership so the sidebar does not
// need to probe the organization API; accounts outside any organization get nil.
func TestOrganizationSelfDataExposesActiveMembership(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	orgId := createOrganizationWithAdmin(t, "Self Co", "self-admin")
	outsiderId := createOrganizationTestUser(t, db, "outsider", common.UserStatusEnabled)

	adminId := userByName(t, db, "self-admin").Id
	membership := selfOrganizationMembership(adminId)
	require.NotNil(t, membership)
	assert.Equal(t, orgId, membership["id"])
	assert.Equal(t, model.OrganizationRoleAdmin, membership["role"])
	assert.Equal(t, model.OrganizationMembershipActive, membership["status"])

	assert.Nil(t, selfOrganizationMembership(outsiderId))
}

func TestOrganizationLifecycleAndLastAdminProtection(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	createOrganizationWithAdmin(t, "Acme", "alice")

	alice := userByName(t, db, "alice")
	assert.Equal(t, common.RoleCommonUser, alice.Role, "the first admin is an ordinary account, not a platform admin")
	assert.True(t, alice.MustChangePassword, "the initial password must be changed at first login")

	// Root can suspend and resume through the status route; cancelling is not a status.
	orgId := createOrganizationWithAdmin(t, "Second", "second-admin")
	recorder := doOrganizationRequest(t, organizationTestRootId, http.MethodPut,
		fmt.Sprintf("/api/organization/%d/status", orgId), `{"status":"cancelled"}`)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	// The admin creates a member account with an initial password.
	_, status := organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPost, "/api/organization/self/members",
		`{"username":"bob","password":"`+organizationTestPass+`"}`))
	require.Equal(t, http.StatusOK, status)
	bob := userByName(t, db, "bob")
	assert.True(t, bob.MustChangePassword)
	assert.Equal(t, common.UserStatusEnabled, bob.Status)

	// A member can read its own organization but cannot manage members.
	_, status = organizationResponseData(t, doOrganizationRequest(t, bob.Id, http.MethodGet, "/api/organization/self/members", ""))
	assert.Equal(t, http.StatusForbidden, status)

	// The only active admin cannot demote itself.
	recorder = doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", alice.Id), `{"role":"member"}`)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "ORGANIZATION_LAST_ADMIN", organizationErrorCode(t, recorder))

	// After promoting bob, alice can step down.
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", bob.Id), `{"role":"admin"}`))
	require.Equal(t, http.StatusOK, status)
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", alice.Id), `{"role":"member"}`))
	assert.Equal(t, http.StatusOK, status)
}

func TestOrganizationMemberDisableEnableResetAndDelete(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	createOrganizationWithAdmin(t, "Ops", "alice")
	alice := userByName(t, db, "alice")
	_, status := organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPost, "/api/organization/self/members",
		`{"username":"carol","password":"`+organizationTestPass+`"}`))
	require.Equal(t, http.StatusOK, status)
	carol := userByName(t, db, "carol")

	// Disabling a member disables the account as well (D12).
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", carol.Id), `{"status":"disabled"}`))
	require.Equal(t, http.StatusOK, status)
	carol = userByName(t, db, "carol")
	assert.Equal(t, common.UserStatusDisabled, carol.Status)

	// Re-enabling restores the account.
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", carol.Id), `{"status":"active"}`))
	require.Equal(t, http.StatusOK, status)
	carol = userByName(t, db, "carol")
	assert.Equal(t, common.UserStatusEnabled, carol.Status)

	// A reset sets a new initial password and requires another change at login.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", carol.Id).Update("must_change_password", false).Error)
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPost,
		fmt.Sprintf("/api/organization/self/members/%d/password", carol.Id), `{"password":"reset-pass-22"}`))
	require.Equal(t, http.StatusOK, status)
	carol = userByName(t, db, "carol")
	assert.True(t, carol.MustChangePassword)
	assert.True(t, common.ValidatePasswordAndHash("reset-pass-22", carol.Password))

	// Deleting a member removes the account and the membership together.
	recorder := doOrganizationRequest(t, alice.Id, http.MethodDelete, fmt.Sprintf("/api/organization/self/members/%d", carol.Id), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var accounts, memberships int64
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", carol.Id).Count(&accounts).Error)
	require.NoError(t, db.Model(&model.OrganizationMembership{}).Where("user_id = ?", carol.Id).Count(&memberships).Error)
	assert.Zero(t, accounts)
	assert.Zero(t, memberships)

	// The last active admin cannot be deleted, even by itself.
	recorder = doOrganizationRequest(t, alice.Id, http.MethodDelete, fmt.Sprintf("/api/organization/self/members/%d", alice.Id), "")
	assert.Equal(t, "ORGANIZATION_LAST_ADMIN", organizationErrorCode(t, recorder))
}

func TestOrganizationFirstLoginPasswordChange(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	createOrganizationWithAdmin(t, "Gate", "dave")
	dave := userByName(t, db, "dave")

	change := func(old string, next string) *httptest.ResponseRecorder {
		return doOrganizationRequest(t, dave.Id, http.MethodPut, "/api/user/self/password",
			fmt.Sprintf(`{"old_password":%q,"new_password":%q}`, old, next))
	}

	// A wrong current password is refused and leaves the flag set.
	recorder := change("wrong-password", "new-password-9")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "AUTH_CURRENT_PASSWORD_INVALID", organizationErrorCode(t, recorder))

	// Reusing the current password is refused.
	recorder = change(organizationTestPass, organizationTestPass)
	assert.Equal(t, "AUTH_PASSWORD_UNCHANGED", organizationErrorCode(t, recorder))

	// Too-short passwords are refused.
	recorder = change(organizationTestPass, "short")
	assert.Equal(t, "AUTH_PASSWORD_INVALID", organizationErrorCode(t, recorder))

	dave = userByName(t, db, "dave")
	require.True(t, dave.MustChangePassword)
	authBefore := dave.AuthVersion

	// A valid change clears the flag, stores the new hash, revokes old sessions,
	// and asks for a fresh sign-in.
	recorder = change(organizationTestPass, "new-password-9")
	data, status := organizationResponseData(t, recorder)
	require.Equal(t, http.StatusOK, status, recorder.Body.String())
	assert.Contains(t, string(data), `"relogin_required":true`)
	dave = userByName(t, db, "dave")
	assert.False(t, dave.MustChangePassword)
	assert.True(t, common.ValidatePasswordAndHash("new-password-9", dave.Password))
	assert.Greater(t, dave.AuthVersion, authBefore)
}

func TestOrganizationTenantBoundariesIgnoreForeignMemberIds(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	createOrganizationWithAdmin(t, "alice-co", "alice")
	createOrganizationWithAdmin(t, "hank-co", "hank")
	alice := userByName(t, db, "alice")
	hank := userByName(t, db, "hank")

	// Alice can act only on members of her own organization, so hank is not found.
	recorder := doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", hank.Id), `{"status":"disabled"}`)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	recorder = doOrganizationRequest(t, alice.Id, http.MethodDelete,
		fmt.Sprintf("/api/organization/self/members/%d", hank.Id), "")
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	recorder = doOrganizationRequest(t, alice.Id, http.MethodPost,
		fmt.Sprintf("/api/organization/self/members/%d/password", hank.Id), `{"password":"reset-pass-22"}`)
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	// Alice's own view shows her organization only.
	data, status := organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodGet, "/api/organization/self", ""))
	require.Equal(t, http.StatusOK, status)
	var self struct {
		Organization model.Organization `json:"organization"`
		Role         string             `json:"role"`
	}
	require.NoError(t, common.Unmarshal(data, &self))
	assert.Equal(t, "alice-co", self.Organization.Name)
	assert.Equal(t, model.OrganizationRoleAdmin, self.Role)
}

func TestOrganizationErrorCodesAreStable(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	orgId := createOrganizationWithAdmin(t, "Codes", "alice")
	alice := userByName(t, db, "alice")
	_, status := organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPost, "/api/organization/self/members",
		`{"username":"bob","password":"`+organizationTestPass+`"}`))
	require.Equal(t, http.StatusOK, status)
	bob := userByName(t, db, "bob")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		user   int
		status int
		code   string
	}{
		{"last admin demotion", http.MethodPut, fmt.Sprintf("/api/organization/self/members/%d", alice.Id), `{"role":"member"}`,
			alice.Id, http.StatusConflict, "ORGANIZATION_LAST_ADMIN"},
		{"member cannot list members", http.MethodGet, "/api/organization/self/members", "",
			bob.Id, http.StatusForbidden, "ORGANIZATION_PERMISSION_DENIED"},
		{"status transition not allowed", http.MethodPut, fmt.Sprintf("/api/organization/%d/status", orgId), `{"status":"cancelled"}`,
			organizationTestRootId, http.StatusBadRequest, "ORGANIZATION_INVALID_STATUS"},
		{"caller without membership", http.MethodGet, "/api/organization/self", "",
			organizationTestRootId, http.StatusNotFound, "ORGANIZATION_MEMBERSHIP_NOT_FOUND"},
		{"unknown member", http.MethodDelete, "/api/organization/self/members/9999", "",
			alice.Id, http.StatusNotFound, "ORGANIZATION_MEMBERSHIP_NOT_FOUND"},
		{"invalid role value", http.MethodPut, fmt.Sprintf("/api/organization/self/members/%d", bob.Id), `{"role":"owner"}`,
			alice.Id, http.StatusBadRequest, "ORGANIZATION_INVALID_ROLE"},
		{"duplicate username", http.MethodPost, "/api/organization/self/members", `{"username":"bob","password":"` + organizationTestPass + `"}`,
			alice.Id, http.StatusConflict, "ORGANIZATION_USERNAME_TAKEN"},
		{"short initial password", http.MethodPost, "/api/organization/self/members", `{"username":"erin","password":"short"}`,
			alice.Id, http.StatusBadRequest, "ORGANIZATION_PASSWORD_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := doOrganizationRequest(t, tc.user, tc.method, tc.path, tc.body)
			assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, tc.code, organizationErrorCode(t, recorder))
		})
	}
}

// Two active admins demote each other at the same time. The organization row
// lock must serialize the two transactions, so exactly one succeeds and the
// organization keeps an active admin.
func TestOrganizationConcurrentAdminDemotionKeepsOneAdmin(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	orgId := createOrganizationWithAdmin(t, "Race", "alice")
	alice := userByName(t, db, "alice")
	_, status := organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPost, "/api/organization/self/members",
		`{"username":"bob","password":"`+organizationTestPass+`"}`))
	require.Equal(t, http.StatusOK, status)
	bob := userByName(t, db, "bob")
	_, status = organizationResponseData(t, doOrganizationRequest(t, alice.Id, http.MethodPut,
		fmt.Sprintf("/api/organization/self/members/%d", bob.Id), `{"role":"admin"}`))
	require.Equal(t, http.StatusOK, status)

	demoteToMember := model.OrganizationRoleMember
	errs := make(chan error, 2)
	start := make(chan struct{})
	for _, userId := range []int{alice.Id, bob.Id} {
		go func() {
			<-start
			errs <- model.UpdateOrganizationMember(orgId, userId, &demoteToMember, nil, common.GetTimestamp())
		}()
	}
	close(start)

	var succeeded, refused int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, model.ErrOrganizationLastAdmin):
			refused++
		default:
			t.Fatalf("unexpected error from concurrent demotion: %v", err)
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, refused)
	views, err := model.ListOrganizationMemberViews(orgId)
	require.NoError(t, err)
	activeAdmins := 0
	for _, view := range views {
		if view.Role == model.OrganizationRoleAdmin && view.Status == model.OrganizationMembershipActive {
			activeAdmins++
		}
	}
	assert.Equal(t, 1, activeAdmins)
}

// The unique user_id index is the database-level guarantee that a user belongs
// to at most one organization, independent of the service checks.
func TestOrganizationMembershipRejectsSecondOrganizationForSameUser(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	orgA := createOrganizationWithAdmin(t, "A", "alice")
	alice := userByName(t, db, "alice")
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Organization{}, &model.OrganizationMembership{}), "a repeated migration must be a no-op")
	err := db.Create(&model.OrganizationMembership{
		OrganizationId: orgA,
		UserId:         alice.Id,
		Role:           model.OrganizationRoleMember,
		Status:         model.OrganizationMembershipActive,
	}).Error
	assert.Error(t, err)
}

// An administrator can delete a user who belongs to an organization: the membership is
// removed together with the account. Self-service deletion stays blocked while the user
// belongs to an organization.
func TestOrganizationMemberDeletedByAdministratorLeavesTheOrganization(t *testing.T) {
	db := setupOrganizationTestDB(t)
	createOrganizationTestUser(t, db, "root", common.UserStatusEnabled)
	createOrganizationWithAdmin(t, "Guarded", "alice")
	alice := userByName(t, db, "alice")

	selfDelete := &model.User{Id: alice.Id}
	_, err := selfDelete.Delete()
	require.ErrorIs(t, err, model.ErrUserInOrganization)

	_, err = model.HardDeleteUserById(alice.Id)
	require.NoError(t, err)

	var remaining int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("id = ?", alice.Id).Count(&remaining).Error)
	assert.Equal(t, int64(0), remaining)
	var memberships int64
	require.NoError(t, db.Model(&model.OrganizationMembership{}).Where("user_id = ?", alice.Id).Count(&memberships).Error)
	assert.Equal(t, int64(0), memberships)
}

func TestOrganizationDeleteEndpointBlocksUntilClearedThenSoftDeletes(t *testing.T) {
	db := setupOrganizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.AuditLog{}))
	previousLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = previousLogDB })

	organizationId := createOrganizationWithAdmin(t, "Delete Co", "delete-admin")
	path := fmt.Sprintf("/api/organization/%d", organizationId)

	recorder := doOrganizationRequest(t, organizationTestRootId, http.MethodDelete, path, "")
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "ORGANIZATION_DELETE_NOT_SUSPENDED", organizationErrorCode(t, recorder), "an active organization cannot be deleted")

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPut, path+"/status", `{"status":"suspended"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPost, path+"/quota/grant",
		`{"amount":1000,"reason":"initial","reference":"grant-delete-blocked"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodGet, path+"/deletion-impact", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"unallocated_quota":1000`)
	assert.NotContains(t, recorder.Body.String(), "unallocated_quota_remaining", "a leftover balance does not block deletion")

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodDelete, path, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	deleted, err := model.GetOrganizationById(organizationId)
	require.NoError(t, err)
	assert.Equal(t, model.OrganizationStatusDeleted, deleted.Status)

	emptyId := createOrganizationWithAdmin(t, "Empty Co", "empty-admin")
	emptyPath := fmt.Sprintf("/api/organization/%d", emptyId)
	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPut, emptyPath+"/status", `{"status":"suspended"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodDelete, emptyPath, "")
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	organization, err := model.GetOrganizationById(emptyId)
	require.NoError(t, err)
	assert.Equal(t, model.OrganizationStatusDeleted, organization.Status)
}

func TestOrganizationQuotaAdjustEndpointAddsAndRefusesOverdraw(t *testing.T) {
	db := setupOrganizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.AuditLog{}))
	organizationId := createOrganizationWithAdmin(t, "Adjust Co", "adjust-admin")
	path := fmt.Sprintf("/api/organization/%d/quota/adjust", organizationId)

	recorder := doOrganizationRequest(t, organizationTestRootId, http.MethodPost, path,
		`{"mode":"add","amount":1000,"reason":"top up","reference":"endpoint-add"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"unallocated":1000`)

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPost, path,
		`{"mode":"subtract","amount":1500,"reason":"too much","reference":"endpoint-subtract"}`)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "ORGANIZATION_QUOTA_INSUFFICIENT", organizationErrorCode(t, recorder))
}
