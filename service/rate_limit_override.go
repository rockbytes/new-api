package service

import (
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// organizationTotalRateLimits maps an organization ID to its request limit per
// window. It is replaced as a whole on each refresh, so readers never lock.
var organizationTotalRateLimits atomic.Pointer[map[int]int]

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

// RefreshRateLimitOverrides reloads the organization limits from the database.
func RefreshRateLimitOverrides() error {
	overrides, err := model.ListRateLimitOverrides(model.RateLimitOverrideSubjectOrganization)
	if err != nil {
		return err
	}
	limits := make(map[int]int, len(overrides))
	for _, override := range overrides {
		limits[override.SubjectId] = override.TotalCount
	}
	ReplaceOrganizationTotalRateLimits(limits)
	return nil
}

// SyncRateLimitOverrides refreshes the organization limits every frequency
// seconds, so a change made on one node reaches the others within one period.
func SyncRateLimitOverrides(frequency int) {
	for {
		if err := RefreshRateLimitOverrides(); err != nil {
			common.SysLog(common.LogText("failed to refresh rate limit overrides: %v", err))
		}
		time.Sleep(time.Duration(frequency) * time.Second)
	}
}
