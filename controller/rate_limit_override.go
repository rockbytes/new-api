package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// maxRateLimitOverrideCount matches the largest model request limit the system
// settings accept, so a window product cannot overflow.
const maxRateLimitOverrideCount = 100000000

type rateLimitOverrideRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectId   int    `json:"subject_id"`
	TotalCount  int    `json:"total_count"`
}

type userRateLimitOverrideRequest struct {
	TotalCount   int `json:"total_count"`
	SuccessCount int `json:"success_count"`
}

// userRateLimitOverrideView is what the user's request-limit panel shows. The counts are
// the override, where 0 inherits the global default. The effective counts are the
// limits that apply now, where 0 is off.
type userRateLimitOverrideView struct {
	UserId           int `json:"user_id"`
	TotalCount       int `json:"total_count"`
	SuccessCount     int `json:"success_count"`
	DefaultTotal     int `json:"default_total"`
	DefaultSuccess   int `json:"default_success"`
	EffectiveTotal   int `json:"effective_total"`
	EffectiveSuccess int `json:"effective_success"`
}

// GetRateLimitOverrides lists the request-limit overrides of one subject type.
// Only root can call it.
func GetRateLimitOverrides(c *gin.Context) {
	subjectType := c.DefaultQuery("subject_type", model.RateLimitOverrideSubjectOrganization)
	if subjectType != model.RateLimitOverrideSubjectOrganization {
		common.ApiErrorMsg(c, "unsupported subject type")
		return
	}
	overrides, err := model.ListRateLimitOverrides(subjectType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overrides)
}

// UpdateRateLimitOverride sets the total request limit of one organization. Only
// root can call it, and the change reaches every node within one sync period.
func UpdateRateLimitOverride(c *gin.Context) {
	var req rateLimitOverrideRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid parameters")
		return
	}
	if req.SubjectType != model.RateLimitOverrideSubjectOrganization {
		common.ApiErrorMsg(c, "unsupported subject type")
		return
	}
	if req.SubjectId <= 0 {
		common.ApiErrorMsg(c, "invalid organization id")
		return
	}
	if req.TotalCount < 1 || req.TotalCount > maxRateLimitOverrideCount {
		common.ApiErrorMsg(c, "total_count must be between 1 and 100000000")
		return
	}
	if _, err := model.GetOrganizationById(req.SubjectId); err != nil {
		common.ApiErrorMsg(c, "organization does not exist")
		return
	}

	override := &model.RateLimitOverride{
		SubjectType: req.SubjectType,
		SubjectId:   req.SubjectId,
		GroupName:   "",
		TotalCount:  req.TotalCount,
		UpdatedBy:   c.GetInt("id"),
		UpdatedTime: common.GetTimestamp(),
	}
	if err := model.UpsertRateLimitOverride(override); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.RefreshRateLimitOverrides(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, override)
}

// DeleteRateLimitOverride removes the total request limit of one organization.
// Only root can call it.
func DeleteRateLimitOverride(c *gin.Context) {
	subjectType := c.Param("subject_type")
	if subjectType != model.RateLimitOverrideSubjectOrganization {
		common.ApiErrorMsg(c, "unsupported subject type")
		return
	}
	subjectId, err := strconv.Atoi(c.Param("subject_id"))
	if err != nil || subjectId <= 0 {
		common.ApiErrorMsg(c, "invalid organization id")
		return
	}
	if err := model.DeleteRateLimitOverride(subjectType, subjectId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.RefreshRateLimitOverrides(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetUserRateLimitOverride returns the request limits an administrator set for one
// user, with the limits that apply to the user now.
func GetUserRateLimitOverride(c *gin.Context) {
	userId, ok := parseRateLimitTargetUserId(c)
	if !ok {
		return
	}
	if _, err := model.GetUserById(userId, false); err != nil {
		common.ApiError(c, err)
		return
	}
	override, err := model.GetRateLimitOverride(model.RateLimitOverrideSubjectUser, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view := userRateLimitOverrideView{UserId: userId}
	if override != nil {
		view.TotalCount, view.SuccessCount = override.TotalCount, override.SuccessCount
	}
	view.DefaultTotal, view.DefaultSuccess = service.GlobalUserRateLimits()
	view.EffectiveTotal, view.EffectiveSuccess = service.EffectiveUserRateLimits(userId)
	common.ApiSuccess(c, view)
}

// UpdateUserRateLimitOverride sets the request limits of one user. An administrator
// can change only users of a lower role than their own; root can change any user.
func UpdateUserRateLimitOverride(c *gin.Context) {
	userId, ok := parseRateLimitTargetUserId(c)
	if !ok {
		return
	}
	var req userRateLimitOverrideRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid parameters")
		return
	}
	if !validUserRateLimitCount(req.TotalCount) || !validUserRateLimitCount(req.SuccessCount) {
		common.ApiErrorMsg(c, "request limits must be between 0 and 100000000")
		return
	}
	if req.TotalCount == 0 && req.SuccessCount == 0 {
		common.ApiErrorMsg(c, "set at least one request limit, or remove the override")
		return
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		return
	}

	override := &model.RateLimitOverride{
		SubjectType:  model.RateLimitOverrideSubjectUser,
		SubjectId:    userId,
		GroupName:    "",
		TotalCount:   req.TotalCount,
		SuccessCount: req.SuccessCount,
		UpdatedBy:    c.GetInt("id"),
		UpdatedTime:  common.GetTimestamp(),
	}
	if err := model.UpsertRateLimitOverride(override); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.RefreshRateLimitOverrides(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.rate_limit_set", map[string]any{
		"username": user.Username,
		"total":    req.TotalCount,
		"success":  req.SuccessCount,
	})
	common.ApiSuccess(c, nil)
}

// DeleteUserRateLimitOverride removes the request limits of one user, so the global
// defaults apply again. The same role rule as the update applies.
func DeleteUserRateLimitOverride(c *gin.Context) {
	userId, ok := parseRateLimitTargetUserId(c)
	if !ok {
		return
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to update users of same or higher permission level")
		return
	}
	if err := model.DeleteRateLimitOverride(model.RateLimitOverrideSubjectUser, userId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.RefreshRateLimitOverrides(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.rate_limit_clear", map[string]any{
		"username": user.Username,
	})
	common.ApiSuccess(c, nil)
}

func parseRateLimitTargetUserId(c *gin.Context) (int, bool) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return 0, false
	}
	return userId, true
}

func validUserRateLimitCount(count int) bool {
	return count >= 0 && count <= maxRateLimitOverrideCount
}
