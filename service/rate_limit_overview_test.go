package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The overview reports each subject's effective limits and writes nothing.
func TestRateLimitOverviewReportsTheEffectiveLimits(t *testing.T) {
	previousMinutes, previousEnabled := setting.ModelRequestRateLimitDurationMinutes, setting.ModelRequestRateLimitEnabled
	previousCount, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	t.Cleanup(func() {
		setting.ModelRequestRateLimitDurationMinutes, setting.ModelRequestRateLimitEnabled = previousMinutes, previousEnabled
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousCount, previousSuccess
		ReplaceOrganizationTotalRateLimits(nil)
	})
	setting.ModelRequestRateLimitDurationMinutes = 2
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitCount = 100
	setting.ModelRequestRateLimitSuccessCount = 50
	ReplaceOrganizationTotalRateLimits(map[int]int{5: 7})

	org, err := BuildRateLimitOverview(RateLimitOverviewSubjectOrganization, 5)
	require.NoError(t, err)
	assert.Equal(t, []RateLimitOverviewLayer{{Layer: "organization", Enabled: true, Limit: 7, WindowSeconds: 120}}, org.Layers)

	orgWithout, err := BuildRateLimitOverview(RateLimitOverviewSubjectOrganization, 6)
	require.NoError(t, err)
	assert.Equal(t, []RateLimitOverviewLayer{{Layer: "organization", WindowSeconds: 120}}, orgWithout.Layers)

	user, err := BuildRateLimitOverview(RateLimitOverviewSubjectUser, 9)
	require.NoError(t, err)
	assert.Equal(t, []RateLimitOverviewLayer{
		{Layer: "user_total", Enabled: true, Limit: 100, WindowSeconds: 120},
		{Layer: "user_success", Enabled: true, Limit: 50, WindowSeconds: 120},
	}, user.Layers)

	setting.ModelRequestRateLimitEnabled = false
	userOff, err := BuildRateLimitOverview(RateLimitOverviewSubjectUser, 9)
	require.NoError(t, err)
	for _, layer := range userOff.Layers {
		assert.False(t, layer.Enabled, "a disabled user limit must not report a value")
		assert.Zero(t, layer.Limit)
	}

	assert.Equal(t, RateLimitOverviewLayer{Layer: "channel", Enabled: true, Limit: 5, WindowSeconds: 30},
		channelOverviewLayer(budgetTestChannel(9801, 1, 5, 30)))
	assert.False(t, channelOverviewLayer(budgetTestChannel(9802, 1, 0, 0)).Enabled)

	_, err = BuildRateLimitOverview("group", 1)
	assert.Error(t, err, "groups are not rate limit layers any more")
}
