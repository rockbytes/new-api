package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// Overview subject types. Each lists the limits that bind that kind of subject.
const (
	RateLimitOverviewSubjectOrganization = "organization"
	RateLimitOverviewSubjectUser         = "user"
	RateLimitOverviewSubjectChannel      = "channel"
)

// RateLimitOverviewLayer is one enforced request limit that binds the subject.
type RateLimitOverviewLayer struct {
	Layer         string `json:"layer"`
	Enabled       bool   `json:"enabled"`
	Limit         int64  `json:"limit"`
	WindowSeconds int64  `json:"window_seconds"`
}

// RateLimitOverview lists the effective limits of one subject. The layers are in the
// order a request meets them: organization, then user, then channel.
type RateLimitOverview struct {
	SubjectType string                   `json:"subject_type"`
	SubjectId   int                      `json:"subject_id"`
	Layers      []RateLimitOverviewLayer `json:"layers"`
}

// BuildRateLimitOverview reads the effective limits of a subject. It writes nothing.
func BuildRateLimitOverview(subjectType string, subjectId int) (*RateLimitOverview, error) {
	view := &RateLimitOverview{SubjectType: subjectType, SubjectId: subjectId}
	switch subjectType {
	case RateLimitOverviewSubjectOrganization:
		view.Layers = []RateLimitOverviewLayer{organizationOverviewLayer(subjectId)}
	case RateLimitOverviewSubjectUser:
		view.Layers = userOverviewLayers()
	case RateLimitOverviewSubjectChannel:
		channel, err := model.GetChannelById(subjectId, false)
		if err != nil {
			return nil, err
		}
		view.Layers = []RateLimitOverviewLayer{channelOverviewLayer(channel)}
	default:
		return nil, fmt.Errorf("unsupported subject type %q", subjectType)
	}
	return view, nil
}

// organizationOverviewLayer reports the organization's total limit. It shares the user
// layer's window, which is why its window comes from the same setting.
func organizationOverviewLayer(organizationId int) RateLimitOverviewLayer {
	layer := RateLimitOverviewLayer{Layer: "organization", WindowSeconds: userLimitWindowSeconds()}
	if limit := int64(OrganizationTotalRateLimit(organizationId)); limit > 0 {
		layer.Enabled, layer.Limit = true, limit
	}
	return layer
}

// userOverviewLayers reports the global user limits. Per-user overrides are not part of
// the overview yet, so every user shows the same values.
func userOverviewLayers() []RateLimitOverviewLayer {
	window := userLimitWindowSeconds()
	total := RateLimitOverviewLayer{Layer: "user_total", WindowSeconds: window}
	if setting.ModelRequestRateLimitEnabled && setting.ModelRequestRateLimitCount > 0 {
		total.Enabled, total.Limit = true, int64(setting.ModelRequestRateLimitCount)
	}
	success := RateLimitOverviewLayer{Layer: "user_success", WindowSeconds: window}
	if setting.ModelRequestRateLimitEnabled && setting.ModelRequestRateLimitSuccessCount > 0 {
		success.Enabled, success.Limit = true, int64(setting.ModelRequestRateLimitSuccessCount)
	}
	return []RateLimitOverviewLayer{total, success}
}

// channelOverviewLayer reports the channel's request budget.
func channelOverviewLayer(channel *model.Channel) RateLimitOverviewLayer {
	requests, window, ok := channelBudget(channel)
	return RateLimitOverviewLayer{Layer: "channel", Enabled: ok, Limit: requests, WindowSeconds: window}
}

func userLimitWindowSeconds() int64 {
	return int64(setting.ModelRequestRateLimitDurationMinutes) * 60
}
