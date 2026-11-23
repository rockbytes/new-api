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
		view.Layers = userOverviewLayers(subjectId)
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

// userOverviewLayers reports the user's effective limits: the administrator's override
// where one is set, otherwise the global default.
func userOverviewLayers(userId int) []RateLimitOverviewLayer {
	window := userLimitWindowSeconds()
	totalMax, successMax := EffectiveUserRateLimits(userId)
	total := RateLimitOverviewLayer{Layer: "user_total", WindowSeconds: window}
	if totalMax > 0 {
		total.Enabled, total.Limit = true, int64(totalMax)
	}
	success := RateLimitOverviewLayer{Layer: "user_success", WindowSeconds: window}
	if successMax > 0 {
		success.Enabled, success.Limit = true, int64(successMax)
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
