/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	usageDefaultWindowSeconds = 7 * 24 * 3600
	usageMaxWindowSeconds     = 400 * 24 * 3600
	usageMaxTzOffsetMinutes   = 14 * 60
	usageDefaultPageSize      = 20
	usageMaxPageSize          = 100
	usageBasis                = "settled_net"
	usageUnit                 = "platform_quota"
)

// usageRequest is a parsed and authorized usage request.
type usageRequest struct {
	resolved service.ResolvedUsageScope
	window   model.UsageWindow
	tzMinute int64
	page     int
	pageSize int
}

// usageScopeInfo describes the scope of a response, so the page can show what it covers.
type usageScopeInfo struct {
	View         service.UsageView `json:"view"`
	Organization *usageOrgInfo     `json:"organization"`
	UserId       int               `json:"user_id"`
}

type usageOrgInfo struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type usageWindowInfo struct {
	StartTimestamp  int64 `json:"start_timestamp"`
	EndTimestamp    int64 `json:"end_timestamp"`
	TzOffsetMinutes int64 `json:"tz_offset_minutes"`
}

// usageResponseMeta is shared by every usage response. It states the consumption basis and
// the unit, so the figures are not read as currency.
type usageResponseMeta struct {
	Scope  usageScopeInfo  `json:"scope"`
	Window usageWindowInfo `json:"window"`
	Basis  string          `json:"basis"`
	Unit   string          `json:"unit"`
}

// parseUsageRequest reads the query, authorizes it for the caller and writes the error
// response itself when the request is not allowed. It returns false in that case.
func parseUsageRequest(c *gin.Context) (usageRequest, bool) {
	var request usageRequest
	organizationId, err := usageIntParam(c, "organization_id")
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid organization_id")
		return request, false
	}
	userId, err := usageIntParam(c, "user_id")
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid user_id")
		return request, false
	}
	request.tzMinute, err = usageRangeParam[int64](c, "tz_offset_minutes", -usageMaxTzOffsetMinutes, usageMaxTzOffsetMinutes, 0)
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid tz_offset_minutes")
		return request, false
	}
	now := time.Now().Unix()
	end, err := usageIntParam64(c, "end_timestamp")
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid end_timestamp")
		return request, false
	}
	if end == 0 {
		end = now
	}
	start, err := usageIntParam64(c, "start_timestamp")
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid start_timestamp")
		return request, false
	}
	if start == 0 {
		start = end - usageDefaultWindowSeconds
	}
	if start > end || end-start > usageMaxWindowSeconds {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_WINDOW", "Invalid time range")
		return request, false
	}
	request.page, err = usageRangeParam[int](c, "page", 1, 1<<30, 1)
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid page")
		return request, false
	}
	request.pageSize, err = usageRangeParam[int](c, "page_size", 1, usageMaxPageSize, usageDefaultPageSize)
	if err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid page_size")
		return request, false
	}
	request.window = model.UsageWindow{Start: start, End: end, TzOffsetSeconds: request.tzMinute * 60}

	view := service.UsageView(c.Query("view"))
	request.resolved, err = service.ResolveUsageScope(c.GetInt("id"), c.GetInt("role"), service.UsageScopeRequest{
		View:           view,
		OrganizationId: organizationId,
		UserId:         userId,
	})
	if err != nil {
		writeUsageScopeError(c, err)
		return request, false
	}
	return request, true
}

func writeUsageScopeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrUsageViewForbidden), errors.Is(err, service.ErrUsageScopeMismatch):
		writeUsageError(c, http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", "This usage view is not available")
	case errors.Is(err, service.ErrUsageOrganizationRequired):
		writeUsageError(c, http.StatusBadRequest, "USAGE_ORGANIZATION_REQUIRED", "Select an organization before selecting a user")
	case errors.Is(err, service.ErrUsageOrganizationNotFound):
		writeUsageError(c, http.StatusNotFound, "USAGE_ORGANIZATION_NOT_FOUND", "Organization not found")
	default:
		common.ApiError(c, err)
	}
}

func writeUsageError(c *gin.Context, status int, code string, message string) {
	common.ApiErrorStatus(c, status, errors.New(message), gin.H{"code": code})
}

func usageIntParam(c *gin.Context, name string) (int, error) {
	value, err := usageIntParam64(c, name)
	return int(value), err
}

func usageIntParam64(c *gin.Context, name string) (int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("invalid " + name)
	}
	return value, nil
}

// usageRangeParam parses an integer that may be negative, within [minimum, maximum]. The
// fallback applies when the parameter is absent.
func usageRangeParam[T ~int | ~int64](c *gin.Context, name string, minimum T, maximum T, fallback T) (T, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || T(value) < minimum || T(value) > maximum {
		return 0, errors.New("invalid " + name)
	}
	return T(value), nil
}

func usageMeta(request usageRequest, organizations map[int]string) usageResponseMeta {
	meta := usageResponseMeta{
		Scope: usageScopeInfo{
			View:   request.resolved.View,
			UserId: request.resolved.Scope.UserId,
		},
		Window: usageWindowInfo{
			StartTimestamp:  request.window.Start,
			EndTimestamp:    request.window.End,
			TzOffsetMinutes: request.tzMinute,
		},
		Basis: usageBasis,
		Unit:  usageUnit,
	}
	if org := request.resolved.Organization; org != nil {
		meta.Scope.Organization = &usageOrgInfo{Id: org.Id, Name: organizations[org.Id]}
	}
	return meta
}

// usageOrganizationNames loads names for the given organization ids in one query.
func usageOrganizationNames(ids []int) (map[int]string, error) {
	names := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var organizations []model.Organization
	if err := model.DB.Select("id", "name").Where("id IN ?", ids).Find(&organizations).Error; err != nil {
		return nil, err
	}
	for _, organization := range organizations {
		names[organization.Id] = organization.Name
	}
	return names, nil
}

// GetUsageSummary returns the net consumption and request totals of the caller's scope.
func GetUsageSummary(c *gin.Context) {
	request, ok := parseUsageRequest(c)
	if !ok {
		return
	}
	summary, err := model.GetUsageSummary(request.resolved.Scope, request.window)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	names, err := usageOrganizationNames(usageOrganizationIds(request))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": usageMeta(request, names), "summary": summary})
}

// GetUsageTrend returns the net consumption per local day of the caller's scope.
func GetUsageTrend(c *gin.Context) {
	request, ok := parseUsageRequest(c)
	if !ok {
		return
	}
	points, err := model.GetUsageTrend(request.resolved.Scope, request.window)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	names, err := usageOrganizationNames(usageOrganizationIds(request))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": usageMeta(request, names), "points": points})
}

// GetUsageModels returns the net consumption per model of the caller's scope.
func GetUsageModels(c *gin.Context) {
	request, ok := parseUsageRequest(c)
	if !ok {
		return
	}
	rows, err := model.GetUsageModels(request.resolved.Scope, request.window)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	names, err := usageOrganizationNames(usageOrganizationIds(request))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": usageMeta(request, names), "models": rows})
}

// usageRankingBy returns what a ranking groups by for the view. Platform reports rank
// organizations across the platform and users inside one organization. The organization
// view ranks its members. A personal view has no ranking.
func usageRankingBy(request usageRequest) (model.UsageRankBy, bool) {
	switch request.resolved.View {
	case service.UsageViewPlatform:
		if request.resolved.Scope.OrganizationId == 0 {
			return model.UsageRankByOrganization, true
		}
		return model.UsageRankByUser, true
	case service.UsageViewOrganization:
		return model.UsageRankByUser, true
	default:
		return "", false
	}
}

// GetUsageRankings returns the largest consumers of the caller's scope.
func GetUsageRankings(c *gin.Context) {
	request, ok := parseUsageRequest(c)
	if !ok {
		return
	}
	by, ranked := usageRankingBy(request)
	rows := []model.UsageRankRow{}
	if ranked {
		var err error
		rows, err = model.GetUsageRanking(request.resolved.Scope, request.window, by)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	// Organization keys are names in the organization ranking, and 0 is personal billing.
	organizationIds := usageOrganizationIds(request)
	if by == model.UsageRankByOrganization {
		for _, row := range rows {
			organizationIds = append(organizationIds, int(row.Key))
		}
	}
	names, err := usageOrganizationNames(organizationIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if by == model.UsageRankByOrganization {
		for i := range rows {
			rows[i].Name = names[int(rows[i].Key)]
		}
	}
	common.ApiSuccess(c, gin.H{"meta": usageMeta(request, names), "rank_by": by, "rows": rows})
}

// usageOrganizationIds lists the organization ids a response may need names for.
func usageOrganizationIds(request usageRequest) []int {
	if request.resolved.Organization == nil {
		return nil
	}
	return []int{request.resolved.Organization.Id}
}

// usageCompanyRecord is the record shape for organization administrators. It does not
// include prompts, responses, IP addresses, channels, upstream ids or operator metadata.
type usageCompanyRecord struct {
	Id               int    `json:"id"`
	CreatedAt        int64  `json:"created_at"`
	Type             int    `json:"type"`
	Username         string `json:"username"`
	TokenName        string `json:"token_name"`
	ModelName        string `json:"model_name"`
	Quota            int    `json:"quota"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	UseTime          int    `json:"use_time"`
	IsStream         bool   `json:"is_stream"`
	RequestId        string `json:"request_id"`
}

// usagePlatformRecord adds the platform operations fields to the company record. Only the
// platform views return it.
type usagePlatformRecord struct {
	usageCompanyRecord
	UserId                int    `json:"user_id"`
	TokenId               int    `json:"token_id"`
	Group                 string `json:"group"`
	ChannelId             int    `json:"channel_id"`
	Ip                    string `json:"ip"`
	BillingOrganizationId int    `json:"billing_organization_id"`
}

// usageSelfRecord adds the payer to the company record, so a member can see which of their
// requests the company paid for.
type usageSelfRecord struct {
	usageCompanyRecord
	BillingOrganizationId   int    `json:"billing_organization_id"`
	BillingOrganizationName string `json:"billing_organization_name"`
}

// GetUsageRecords returns one page of records, shaped by the caller's view.
func GetUsageRecords(c *gin.Context) {
	request, ok := parseUsageRequest(c)
	if !ok {
		return
	}
	logs, total, err := model.GetUsageRecords(request.resolved.Scope, request.window, request.page, request.pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var organizationIds []int
	if request.resolved.View == service.UsageViewSelf {
		for _, log := range logs {
			organizationIds = append(organizationIds, log.BillingOrganizationId)
		}
	}
	organizationIds = append(organizationIds, usageOrganizationIds(request)...)
	names, err := usageOrganizationNames(organizationIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]any, 0, len(logs))
	for _, log := range logs {
		company := usageCompanyRecord{
			Id: log.Id, CreatedAt: log.CreatedAt, Type: log.Type, Username: log.Username, TokenName: log.TokenName,
			ModelName: log.ModelName, Quota: log.Quota, PromptTokens: log.PromptTokens, CompletionTokens: log.CompletionTokens,
			UseTime: log.UseTime, IsStream: log.IsStream, RequestId: log.RequestId,
		}
		switch request.resolved.View {
		case service.UsageViewPlatform:
			items = append(items, usagePlatformRecord{
				usageCompanyRecord: company, UserId: log.UserId, TokenId: log.TokenId, Group: log.Group,
				ChannelId: log.ChannelId, Ip: log.Ip, BillingOrganizationId: log.BillingOrganizationId,
			})
		case service.UsageViewOrganization:
			items = append(items, company)
		default:
			items = append(items, usageSelfRecord{
				usageCompanyRecord:      company,
				BillingOrganizationId:   log.BillingOrganizationId,
				BillingOrganizationName: names[log.BillingOrganizationId],
			})
		}
	}
	common.ApiSuccess(c, gin.H{
		"meta":      usageMeta(request, names),
		"total":     total,
		"page":      request.page,
		"page_size": request.pageSize,
		"items":     items,
	})
}
