package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An administrator sets the request limits of a user of a lower role only, and root sets
// any user's. A write must carry at least one count within range. Delete follows the same
// role rule.
func TestUserRateLimitOverrideRoleRulesAndValidation(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupOrganizationTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}, &model.RateLimitOverride{}))
	previousLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
		service.ReplaceUserRateLimitOverrides(nil)
	})

	commonUserId := createRateLimitTestUser(t, "rl-common", common.RoleCommonUser)
	adminUserId := createRateLimitTestUser(t, "rl-admin", common.RoleAdminUser)
	rootUserId := createRateLimitTestUser(t, "rl-root", common.RoleRootUser)

	body := `{"total_count":5,"success_count":0}`
	assert.Equal(t, true, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodPut, commonUserId, body)["success"],
		"an administrator sets a user of a lower role")
	override, ok := service.UserRateLimitOverrideOf(commonUserId)
	require.True(t, ok)
	assert.Equal(t, 5, override.TotalCount)

	assert.Equal(t, false, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodPut, adminUserId, body)["success"],
		"an administrator cannot set another administrator")
	assert.Equal(t, false, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodPut, rootUserId, body)["success"],
		"an administrator cannot set root")
	_, ok = service.UserRateLimitOverrideOf(adminUserId)
	assert.False(t, ok, "a refused write must not install an override")

	assert.Equal(t, true, callUserRateLimitOverride(t, common.RoleRootUser, http.MethodPut, adminUserId, body)["success"],
		"root sets an administrator")

	assert.Equal(t, false, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodPut, commonUserId,
		`{"total_count":0,"success_count":0}`)["success"], "a write with no positive count is refused")
	assert.Equal(t, false, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodPut, commonUserId,
		`{"total_count":100000001,"success_count":0}`)["success"], "a count above the maximum is refused")

	assert.Equal(t, false, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodDelete, adminUserId, "")["success"],
		"an administrator cannot remove another administrator's override")
	assert.Equal(t, true, callUserRateLimitOverride(t, common.RoleAdminUser, http.MethodDelete, commonUserId, "")["success"])
	_, ok = service.UserRateLimitOverrideOf(commonUserId)
	assert.False(t, ok, "removing the override must reach the in-process cache at once")
}

func createRateLimitTestUser(t *testing.T, username string, role int) int {
	t.Helper()
	user := &model.User{Username: username, Password: "not-a-real-password", Role: role, Status: common.UserStatusEnabled, AffCode: "aff-" + username}
	require.NoError(t, model.DB.Create(user).Error)
	return user.Id
}

// callUserRateLimitOverride sends one request through the real handler as an acting
// user of the given role and returns the decoded response.
func callUserRateLimitOverride(t *testing.T, actingRole int, method string, userId int, body string) map[string]any {
	t.Helper()
	router := gin.New()
	actAs := func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", actingRole)
	}
	const path = "/api/user/:id/rate_limit_override"
	switch method {
	case http.MethodPut:
		router.PUT(path, actAs, UpdateUserRateLimitOverride)
	case http.MethodDelete:
		router.DELETE(path, actAs, DeleteUserRateLimitOverride)
	default:
		t.Fatalf("unsupported method %s", method)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/api/user/"+strconv.Itoa(userId)+"/rate_limit_override", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}
