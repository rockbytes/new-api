package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// GetUserUsableGroups returns the groups a user may select, mapped to their
// descriptions. Administrators author the visibility rules, so the rules do not
// bind them: role >= RoleAdminUser sees every group in the pricing table, plus
// Auto while Auto groups are configured. For everyone else nothing is visible
// unless the user group's rules grant it: each "+:<group>" rule makes one group
// visible. "-:" rules from earlier configurations are ignored, so they can never
// widen access.
func GetUserUsableGroups(userGroup string, role int) map[string]string {
	usableGroups := make(map[string]string)
	if role >= common.RoleAdminUser {
		for groupName := range ratio_setting.GetGroupRatioCopy() {
			usableGroups[groupName] = ""
		}
		if len(setting.GetAutoGroups()) > 0 {
			usableGroups["auto"] = ""
		}
		return usableGroups
	}
	if userGroup == "" {
		return usableGroups
	}
	rules, ok := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
	if !ok {
		return usableGroups
	}
	for rule, desc := range rules {
		if after, ok := strings.CutPrefix(rule, "+:"); ok {
			usableGroups[after] = desc
		}
	}
	return usableGroups
}

func GroupInUserUsableGroups(userGroup, groupName string, role int) bool {
	_, ok := GetUserUsableGroups(userGroup, role)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string, role int) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName, role) && ratio_setting.ContainsGroupRatio(groupName)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string, role int) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsUserSelectableGroup(userGroup, group, role) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string, role int) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group, role) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	role := common.GetContextKeyInt(c, constant.ContextKeyUserRole)
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup, role)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups, role)
}

// GetGroupsEnabledModels 按 groups 顺序获取各分组启用的模型并去重
func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; !ok {
				seen[modelName] = struct{}{}
				models = append(models, modelName)
			}
		}
	}
	return models
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
