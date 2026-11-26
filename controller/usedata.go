package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func parseFlowQuotaTimeRange(c *gin.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	startTimestamp, err := limitLogTimeRange(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

type overviewOrganizationAccount struct {
	Id             int    `json:"id"`
	Name           string `json:"name"`
	RemainingQuota *int64 `json:"remaining_quota"`
	RecentUsage    *int64 `json:"recent_usage"`
}

type overviewAccountStatus struct {
	PersonalUsage           *int64                       `json:"personal_usage"`
	Organization            *overviewOrganizationAccount `json:"organization"`
	OrganizationUnavailable bool                         `json:"organization_unavailable,omitempty"`
}

// GetOverviewAccountStatus returns one snapshot for the current account, with no
// client-selected user, payer, role, or time range. A null metric is unavailable, not zero.
func GetOverviewAccountStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userId := c.GetInt("id")
	if userId <= 0 {
		c.Status(http.StatusUnauthorized)
		return
	}
	end := time.Now()
	window := model.UsageWindow{Start: end.Add(-24 * time.Hour).Unix(), End: end.Unix()}
	var status overviewAccountStatus
	personal, err := model.GetUsageSummary(model.UsageScope{UserId: userId, PayerType: "personal"}, window)
	if err != nil {
		common.SysError("overview personal usage: " + err.Error())
	} else {
		status.PersonalUsage = &personal.ConsumeQuota
	}

	// Resolve current membership even when the consumption log is unavailable.
	_, organization, err := model.GetActiveOrganizationMembership(userId)
	if errors.Is(err, model.ErrOrganizationMembershipMissing) {
		common.ApiSuccess(c, status)
		return
	}
	if err != nil || organization.Id <= 0 {
		if err != nil {
			common.SysError("overview organization membership: " + err.Error())
		}
		status.OrganizationUnavailable = true
		common.ApiSuccess(c, status)
		return
	}

	current := overviewOrganizationAccount{Id: organization.Id, Name: organization.Name}
	remaining, err := model.GetOrganizationMemberRemainingQuota(organization.Id, userId)
	if err != nil {
		common.SysError("overview remaining allocation: " + err.Error())
	} else {
		current.RemainingQuota = &remaining
	}
	usage, err := model.GetUsageSummary(model.UsageScope{UserId: userId, OrganizationId: organization.Id}, window)
	if err != nil {
		common.SysError("overview organization usage: " + err.Error())
	} else {
		current.RecentUsage = &usage.ConsumeQuota
	}
	status.Organization = &current
	common.ApiSuccess(c, status)
}

func GetAllFlowQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, username, 0, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}
