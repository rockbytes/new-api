package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setUserGroupVisibleGroups grants userGroup the given groups for the duration of
// the test. The previous rules are restored when the test ends.
func setUserGroupVisibleGroups(t *testing.T, userGroup string, groups map[string]string) {
	t.Helper()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	original := specialGroups.ReadAll()
	t.Cleanup(func() {
		specialGroups.Clear()
		specialGroups.AddAll(original)
	})
	rules := make(map[string]string, len(groups))
	for name, desc := range groups {
		rules["+:"+name] = desc
	}
	specialGroups.Set(userGroup, rules)
}

func configureRequestAutoGroupsTest(t *testing.T) {
	t.Helper()
	originalMax := setting.GetMaxTokenAutoGroups()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default","svip"]`))
	setUserGroupVisibleGroups(t, "default", map[string]string{"default": "Default", "vip": "VIP", "svip": "SVIP"})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1,"svip":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMax)))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func newRequestAutoGroupsContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func TestGetRequestAutoGroupsInheritedListIsNotLimited(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()

	groups := GetRequestAutoGroups(ctx, "default")

	assert.Equal(t, []string{"vip", "default", "svip"}, groups)
}

func TestGetRequestAutoGroupsFiltersBeforeApplyingCurrentLimit(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"revoked", "vip", "default", "svip"})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))

	groups := GetRequestAutoGroups(ctx, "default")

	assert.Equal(t, []string{"vip", "default"}, groups)
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("1"))
	assert.Equal(t, []string{"vip"}, GetRequestAutoGroups(ctx, "default"))
}

func TestGetRequestAutoGroupsDoesNotFallBackAfterPermissionChange(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip"})
	setUserGroupVisibleGroups(t, "default", map[string]string{"default": "Default"})

	groups := GetRequestAutoGroups(ctx, "default")

	assert.Empty(t, groups)
}

func TestGetUserUsableGroupsGrantsOnlyExplicitRules(t *testing.T) {
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	original := specialGroups.ReadAll()
	t.Cleanup(func() {
		specialGroups.Clear()
		specialGroups.AddAll(original)
	})
	specialGroups.Clear()
	specialGroups.Set("team-a", map[string]string{
		"+:team-a":   "Team A",
		"+:supplier": "Supplier",
		"-:other":    "ignored",
	})
	specialGroups.Set("team-c", map[string]string{"+:supplier": "Supplier"})

	assert.Empty(t, GetUserUsableGroups("team-b", common.RoleCommonUser), "a user group without rules sees nothing, not even its own group")
	assert.Empty(t, GetUserUsableGroups("", common.RoleCommonUser))
	assert.Equal(t, map[string]string{"team-a": "Team A", "supplier": "Supplier"}, GetUserUsableGroups("team-a", common.RoleCommonUser))
	assert.Equal(t, map[string]string{"supplier": "Supplier"}, GetUserUsableGroups("team-c", common.RoleCommonUser), "own group is not added unless granted")
	assert.False(t, GroupInUserUsableGroups("team-a", "other", common.RoleCommonUser), "a removal rule never widens access")
}

func TestGetUserUsableGroupsAdministratorsAreNotBoundByRules(t *testing.T) {
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecialGroups := specialGroups.ReadAll()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
	// "ops" grants only a group outside the pricing table, and "team" has no rules.
	specialGroups.Clear()
	specialGroups.Set("ops", map[string]string{"+:ghost": "Not in the pricing table"})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))

	// Administrators author the rules, so they see the whole pricing table plus
	// Auto, and never a rule key that is missing from it.
	expected := map[string]string{"default": "", "vip": "", "auto": ""}
	assert.Equal(t, expected, GetUserUsableGroups("ops", common.RoleAdminUser))
	assert.Equal(t, expected, GetUserUsableGroups("ops", common.RoleRootUser))
	assert.Empty(t, GetUserUsableGroups("team", common.RoleCommonUser), "the rules still bind non-administrators")
	assert.True(t, IsUserSelectableGroup("ops", "vip", common.RoleAdminUser))
	assert.Equal(t, []string{"vip"}, GetUserAutoGroup("ops", common.RoleAdminUser))
	assert.Equal(t, []string{"vip"}, FilterUserTokenAutoGroups("ops", []string{"vip", "ghost"}, common.RoleAdminUser))
}
