package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Fixture accounts. The caller's identity comes from test headers, standing in for the
// authentication middleware that sets "id" and "role" in production.
const (
	usageRootUser   = 14
	usageOrgAAdmin  = 10
	usageOrgAMember = 11
	usageOrgBMember = 12
	usageNoOrgUser  = 13
	usageOrgA       = 1
	usageOrgB       = 2
)

func setupUsageReportEndpoints(t *testing.T) *gin.Engine {
	t.Helper()
	// Fixture rows sit one day in the past, inside the default seven-day window.
	base := time.Now().Unix() - 86400
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/usage-report.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Organization{}, &model.OrganizationMembership{}, &model.Log{}))

	require.NoError(t, db.Create(&model.Organization{Id: usageOrgA, Name: "Org A", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	require.NoError(t, db.Create(&model.Organization{Id: usageOrgB, Name: "Org B", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	memberships := []model.OrganizationMembership{
		{OrganizationId: usageOrgA, UserId: usageOrgAAdmin, Role: model.OrganizationRoleAdmin, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
		{OrganizationId: usageOrgA, UserId: usageOrgAMember, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
		{OrganizationId: usageOrgB, UserId: usageOrgBMember, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
	}
	require.NoError(t, db.Create(&memberships).Error)
	logs := []model.Log{
		{UserId: usageOrgAAdmin, Username: "org-a-admin", Type: model.LogTypeConsume, ModelName: "m1", Quota: 100, TokenName: "k1", RequestId: "a-admin", CreatedAt: base + 60, BillingOrganizationId: usageOrgA, ChannelId: 3, Ip: "10.1.1.1", UseTime: 2, Group: "default"},
		{UserId: usageOrgAMember, Username: "org-a-member", Type: model.LogTypeConsume, ModelName: "m1", Quota: 40, TokenName: "k2", RequestId: "a-member", CreatedAt: base + 120, BillingOrganizationId: usageOrgA, ChannelId: 3, Ip: "10.1.1.2"},
		{UserId: usageOrgAMember, Username: "org-a-member", Type: model.LogTypeConsume, ModelName: "m2", Quota: 15, TokenName: "k3", RequestId: "a-member-personal", CreatedAt: base + 180, BillingOrganizationId: 0, ChannelId: 4, Ip: "10.1.1.3"},
		{UserId: usageOrgBMember, Username: "org-b-member", Type: model.LogTypeConsume, ModelName: "m1", Quota: 70, TokenName: "k4", RequestId: "b-member", CreatedAt: base + 240, BillingOrganizationId: usageOrgB, ChannelId: 5, Ip: "10.2.2.2"},
		{UserId: usageNoOrgUser, Username: "no-org", Type: model.LogTypeConsume, ModelName: "m1", Quota: 9, TokenName: "k5", RequestId: "personal", CreatedAt: base + 300, BillingOrganizationId: 0, ChannelId: 6, Ip: "10.3.3.3"},
	}
	require.NoError(t, db.Create(&logs).Error)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", headerInt(c, "X-Test-User"))
		c.Set("role", headerInt(c, "X-Test-Role"))
		c.Next()
	})
	group := engine.Group("/api/usage")
	group.GET("/summary", GetUsageSummary)
	group.GET("/trend", GetUsageTrend)
	group.GET("/models", GetUsageModels)
	group.GET("/rankings", GetUsageRankings)
	group.GET("/records", GetUsageRecords)
	return engine
}

func headerInt(c *gin.Context, name string) int {
	value, _ := strconv.Atoi(c.GetHeader(name))
	return value
}

// usageCall sends a request as the given user and role. Query parameters are added as-is.
func usageCall(t *testing.T, engine *gin.Engine, user int, role int, path string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Test-User", strconv.Itoa(user))
	request.Header.Set("X-Test-Role", strconv.Itoa(role))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

func usageData(body map[string]any) map[string]any {
	data, _ := body["data"].(map[string]any)
	return data
}

func usageItemKeys(body map[string]any) map[string]bool {
	keys := map[string]bool{}
	items, _ := usageData(body)["items"].([]any)
	if len(items) == 0 {
		return keys
	}
	item, _ := items[0].(map[string]any)
	for key := range item {
		keys[key] = true
	}
	return keys
}

// The company record is the shape organization administrators receive. Platform records add
// the operations fields; self records add the payer. Nothing else is returned.
var (
	usageCompanyKeys = []string{"id", "created_at", "type", "username", "token_name", "model_name", "quota",
		"prompt_tokens", "completion_tokens", "use_time", "is_stream", "request_id"}
	usagePlatformKeys = []string{"user_id", "token_id", "group", "channel_id", "ip", "billing_organization_id"}
	usageSelfKeys     = []string{"billing_organization_id", "billing_organization_name"}
)

func assertKeysExactly(t *testing.T, got map[string]bool, want ...[]string) {
	t.Helper()
	expected := map[string]bool{}
	for _, group := range want {
		for _, key := range group {
			expected[key] = true
		}
	}
	assert.Equal(t, expected, got)
}

func TestUsageReportAuthorizationMatrix(t *testing.T) {
	engine := setupUsageReportEndpoints(t)
	cases := []struct {
		name   string
		user   int
		role   int
		path   string
		status int
		code   string
		view   string
	}{
		{"root sees the whole platform", usageRootUser, common.RoleRootUser, "/api/usage/summary", http.StatusOK, "", "platform"},
		{"platform user needs an organization before a user", usageRootUser, common.RoleRootUser, "/api/usage/summary?user_id=11", http.StatusBadRequest, "USAGE_ORGANIZATION_REQUIRED", ""},
		{"platform organization must exist", usageRootUser, common.RoleRootUser, "/api/usage/summary?organization_id=999", http.StatusNotFound, "USAGE_ORGANIZATION_NOT_FOUND", ""},
		{"organization admin gets their organization by default", usageOrgAAdmin, common.RoleCommonUser, "/api/usage/summary", http.StatusOK, "", "organization"},
		{"organization admin cannot name another organization", usageOrgAAdmin, common.RoleCommonUser, "/api/usage/summary?organization_id=2", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"organization admin cannot open the platform view", usageOrgAAdmin, common.RoleCommonUser, "/api/usage/summary?view=platform", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"member gets their own usage by default", usageOrgAMember, common.RoleCommonUser, "/api/usage/summary", http.StatusOK, "", "self"},
		{"member cannot open the organization view", usageOrgAMember, common.RoleCommonUser, "/api/usage/summary?view=organization", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"member cannot read another user", usageOrgAMember, common.RoleCommonUser, "/api/usage/summary?view=self&user_id=10", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"member cannot name an organization", usageOrgAMember, common.RoleCommonUser, "/api/usage/summary?organization_id=2", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"user without an organization gets personal usage", usageNoOrgUser, common.RoleCommonUser, "/api/usage/summary", http.StatusOK, "", "self"},
		{"an unknown view is refused", usageOrgAMember, common.RoleCommonUser, "/api/usage/summary?view=everything", http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", ""},
		{"a reversed time range is refused", usageRootUser, common.RoleRootUser, "/api/usage/summary?start_timestamp=200&end_timestamp=100", http.StatusBadRequest, "USAGE_INVALID_WINDOW", ""},
		{"an oversized page is refused", usageRootUser, common.RoleRootUser, "/api/usage/records?page_size=500", http.StatusBadRequest, "USAGE_INVALID_REQUEST", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := usageCall(t, engine, tc.user, tc.role, tc.path)
			require.Equal(t, tc.status, status, "body: %v", body)
			if tc.code != "" {
				assert.Equal(t, tc.code, body["code"])
				return
			}
			assert.Equal(t, tc.view, string(usageMetaScopeView(t, body)))
		})
	}
}

func usageMetaScopeView(t *testing.T, body map[string]any) string {
	t.Helper()
	meta, _ := usageData(body)["meta"].(map[string]any)
	scope, _ := meta["scope"].(map[string]any)
	view, _ := scope["view"].(string)
	return view
}

func TestUsageReportTotalsFollowTheCallersScope(t *testing.T) {
	engine := setupUsageReportEndpoints(t)

	_, platform := usageCall(t, engine, usageRootUser, common.RoleRootUser, "/api/usage/summary")
	assert.Equal(t, float64(234), usageData(platform)["summary"].(map[string]any)["net_quota"], "all payers: 100+40+15+70+9")

	_, organization := usageCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, "/api/usage/summary")
	assert.Equal(t, float64(140), usageData(organization)["summary"].(map[string]any)["net_quota"], "organization A pays 100+40")
	assert.Equal(t, float64(2), usageData(organization)["summary"].(map[string]any)["requests"], "two organization-paid requests")

	_, member := usageCall(t, engine, usageOrgAMember, common.RoleCommonUser, "/api/usage/summary")
	assert.Equal(t, float64(55), usageData(member)["summary"].(map[string]any)["net_quota"], "own usage: 40 paid by A plus 15 personal")

	_, personal := usageCall(t, engine, usageNoOrgUser, common.RoleCommonUser, "/api/usage/summary")
	assert.Equal(t, float64(9), usageData(personal)["summary"].(map[string]any)["net_quota"])
}

func TestUsageRecordsExposeOnlyTheWhitelistForEachView(t *testing.T) {
	engine := setupUsageReportEndpoints(t)

	_, platform := usageCall(t, engine, usageRootUser, common.RoleRootUser, "/api/usage/records?page_size=100")
	assertKeysExactly(t, usageItemKeys(platform), usageCompanyKeys, usagePlatformKeys)

	_, organization := usageCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, "/api/usage/records")
	assertKeysExactly(t, usageItemKeys(organization), usageCompanyKeys)

	_, self := usageCall(t, engine, usageOrgAMember, common.RoleCommonUser, "/api/usage/records")
	assertKeysExactly(t, usageItemKeys(self), usageCompanyKeys, usageSelfKeys)
}

func TestUsageSelfRecordsNameThePayer(t *testing.T) {
	engine := setupUsageReportEndpoints(t)
	_, self := usageCall(t, engine, usageOrgAMember, common.RoleCommonUser, "/api/usage/records")
	items, _ := usageData(self)["items"].([]any)
	require.Len(t, items, 2)
	var paidByCompany map[string]any
	for _, raw := range items {
		if item, _ := raw.(map[string]any); item["request_id"] == "a-member" {
			paidByCompany = item
		}
	}
	require.NotNil(t, paidByCompany, "the organization-paid request is in the member records")
	assert.Equal(t, float64(usageOrgA), paidByCompany["billing_organization_id"])
	assert.Equal(t, "Org A", paidByCompany["billing_organization_name"])
}
