package controller

import (
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func userModelRateLimitTarget(c *gin.Context) *model.User {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return nil
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorT(c, "No permission to access users of same or higher level")
		return nil
	}
	return user
}

func respondUserModelRateLimit(c *gin.Context, override *model.UserModelRateLimit) {
	settings := setting.CurrentModelRequestRateLimit()
	common.ApiSuccess(c, gin.H{
		"enabled":          settings.Enabled,
		"duration_minutes": settings.DurationMinutes,
		"defaults":         model.UserModelRateLimit{Total: settings.Total, Success: settings.Success},
		"override":         override,
	})
}

func GetUserModelRateLimit(c *gin.Context) {
	user := userModelRateLimitTarget(c)
	if user == nil {
		return
	}
	override, err := model.GetUserModelRateLimit(c.Request.Context(), user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	respondUserModelRateLimit(c, override)
}

func UpdateUserModelRateLimit(c *gin.Context) {
	user := userModelRateLimitTarget(c)
	if user == nil {
		return
	}
	var request struct {
		Override common.RawMessage `json:"override"`
	}
	const maxBodySize = 4096
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize))
	if err != nil || common.Unmarshal(body, &request) != nil || request.Override == nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var limits *struct {
		Total   *int32 `json:"total"`
		Success *int32 `json:"success"`
	}
	if common.Unmarshal(request.Override, &limits) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var override *model.UserModelRateLimit
	if limits != nil {
		if limits.Total == nil || limits.Success == nil || *limits.Total < 0 || *limits.Success < 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		override = &model.UserModelRateLimit{Total: int(*limits.Total), Success: int(*limits.Success)}
	}
	if err := user.SetModelRateLimit(c.Request.Context(), override); err != nil {
		common.ApiError(c, err)
		return
	}
	respondUserModelRateLimit(c, override)
}
