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
