package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetRateLimitOverview returns the effective request limits of one subject. It only reads:
// the values are edited on their own settings pages. Only root can call it.
func GetRateLimitOverview(c *gin.Context) {
	subjectId, err := strconv.Atoi(c.Query("subject_id"))
	if err != nil || subjectId <= 0 {
		common.ApiErrorMsg(c, "subject_id must be a positive integer")
		return
	}
	view, err := service.BuildRateLimitOverview(c.Query("subject_type"), subjectId)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, view)
}
