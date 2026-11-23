package service

import (
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// organizationTotalRateLimits maps an organization ID to its request limit per
// window. It is replaced as a whole on each refresh, so readers never lock.
var organizationTotalRateLimits atomic.Pointer[map[int]int]

// userRateLimitOverrides maps a user ID to the request limits that user overrides.
// It is replaced as a whole on each refresh, so readers never lock.
var userRateLimitOverrides atomic.Pointer[map[int]UserRateLimitOverride]

// UserRateLimitOverride holds the request limits an administrator sets for one user.
// A count of 0 does not override that limit, so the global default applies.
type UserRateLimitOverride struct {
	TotalCount   int
	SuccessCount int
}

// OrganizationTotalRateLimit returns the organization's request limit per
// window, or 0 when the organization has no override.
func OrganizationTotalRateLimit(organizationId int) int {
	limits := organizationTotalRateLimits.Load()
	if limits == nil {
		return 0
	}
	return (*limits)[organizationId]
}

// ReplaceOrganizationTotalRateLimits installs a new set of organization limits.
// Only entries with a positive count are kept.
func ReplaceOrganizationTotalRateLimits(limits map[int]int) {
	kept := make(map[int]int, len(limits))
	for organizationId, count := range limits {
		if count > 0 {
			kept[organizationId] = count
		}
	}
	organizationTotalRateLimits.Store(&kept)
}

// ReplaceUserRateLimitOverrides installs a new set of user overrides. Only users
// with at least one positive count are kept.
func ReplaceUserRateLimitOverrides(overrides map[int]UserRateLimitOverride) {
	kept := make(map[int]UserRateLimitOverride, len(overrides))
	for userId, override := range overrides {
		if override.TotalCount > 0 || override.SuccessCount > 0 {
			kept[userId] = override
		}
	}
	userRateLimitOverrides.Store(&kept)
}

// UserRateLimitOverrideOf returns the request limits an administrator set for the
// user. The second result is false when the user has no override.
func UserRateLimitOverrideOf(userId int) (UserRateLimitOverride, bool) {
	overrides := userRateLimitOverrides.Load()
	if overrides == nil {
		return UserRateLimitOverride{}, false
	}
	override, ok := (*overrides)[userId]
	return override, ok
}

// GlobalUserRateLimits returns the global total and success limits per window. Both are
// 0 while the global user limit is off.
func GlobalUserRateLimits() (total int, success int) {
	if !setting.ModelRequestRateLimitEnabled {
		return 0, 0
	}
	return setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
}

// EffectiveUserRateLimits returns the total and success request limits that apply
// to a user per window. A limit of 0 is off. An override applies even when the
// global user limit is off. A count the override leaves at 0 takes the global value.
func EffectiveUserRateLimits(userId int) (total int, success int) {
	total, success = GlobalUserRateLimits()
	override, ok := UserRateLimitOverrideOf(userId)
	if !ok {
		return total, success
	}
	if override.TotalCount > 0 {
		total = override.TotalCount
	}
	if override.SuccessCount > 0 {
		success = override.SuccessCount
	}
	return total, success
}

// RefreshRateLimitOverrides reloads the organization and user overrides from the
// database. Both are read before either is installed, so a failed read keeps the
// previous state of both.
func RefreshRateLimitOverrides() error {
	organizationOverrides, err := model.ListRateLimitOverrides(model.RateLimitOverrideSubjectOrganization)
	if err != nil {
		return err
	}
	userOverrides, err := model.ListRateLimitOverrides(model.RateLimitOverrideSubjectUser)
	if err != nil {
		return err
	}

	organizationLimits := make(map[int]int, len(organizationOverrides))
	for _, override := range organizationOverrides {
		organizationLimits[override.SubjectId] = override.TotalCount
	}
	ReplaceOrganizationTotalRateLimits(organizationLimits)

	userLimits := make(map[int]UserRateLimitOverride, len(userOverrides))
	for _, override := range userOverrides {
		userLimits[override.SubjectId] = UserRateLimitOverride{
			TotalCount:   override.TotalCount,
			SuccessCount: override.SuccessCount,
		}
	}
	ReplaceUserRateLimitOverrides(userLimits)
	return nil
}

// SyncRateLimitOverrides refreshes the overrides every frequency seconds, so a
// change made on one node reaches the others within one period.
func SyncRateLimitOverrides(frequency int) {
	for {
		if err := RefreshRateLimitOverrides(); err != nil {
			common.SysLog(common.LogText("failed to refresh rate limit overrides: %v", err))
		}
		time.Sleep(time.Duration(frequency) * time.Second)
	}
}
