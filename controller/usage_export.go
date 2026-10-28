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
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const usageExportListLimit = 50

type usageExportCreateRequest struct {
	View            string `json:"view"`
	OrganizationId  int    `json:"organization_id"`
	UserId          int    `json:"user_id"`
	StartTimestamp  int64  `json:"start_timestamp"`
	EndTimestamp    int64  `json:"end_timestamp"`
	TzOffsetMinutes int    `json:"tz_offset_minutes"`
	Kind            string `json:"kind"`
	Format          string `json:"format"`
	WithBom         bool   `json:"with_bom"`
}

// usageExportResponse is the job as clients see it. The storage key never leaves the server.
// A succeeded job past its expiry reads as expired even before the cleanup task has run.
func usageExportResponse(job *model.UsageExportJob, now int64) gin.H {
	status := job.Status
	if status == model.UsageExportStatusSucceeded && job.ExpiresTime <= now {
		status = model.UsageExportStatusExpired
	}
	response := gin.H{
		"job_id":            job.JobId,
		"status":            status,
		"view":              job.View,
		"organization_id":   job.OrganizationId,
		"target_user_id":    job.TargetUserId,
		"start_timestamp":   job.StartTimestamp,
		"end_timestamp":     job.EndTimestamp,
		"tz_offset_minutes": job.TzOffsetMinutes,
		"kind":              job.Kind,
		"format":            job.Format,
		"with_bom":          job.WithBom,
		"row_count":         job.RowCount,
		"file_name":         job.FileName,
		"error_code":        job.ErrorCode,
		"created_time":      job.CreatedTime,
		"started_time":      job.StartedTime,
		"finished_time":     job.FinishedTime,
		"expires_time":      job.ExpiresTime,
	}
	if status == model.UsageExportStatusSucceeded {
		response["download_url"] = "/api/usage/exports/" + job.JobId + "/download"
	}
	return response
}

// CreateUsageExport authorizes and queues (or completes) one export.
func CreateUsageExport(c *gin.Context) {
	var request usageExportCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid request")
		return
	}
	now := common.GetTimestamp()
	if request.EndTimestamp == 0 {
		request.EndTimestamp = now
	}
	if request.StartTimestamp == 0 {
		request.StartTimestamp = request.EndTimestamp - usageDefaultWindowSeconds
	}
	if request.StartTimestamp < 0 || request.StartTimestamp > request.EndTimestamp ||
		request.EndTimestamp-request.StartTimestamp > usageMaxWindowSeconds {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_WINDOW", "Invalid time range")
		return
	}
	if request.TzOffsetMinutes < -usageMaxTzOffsetMinutes || request.TzOffsetMinutes > usageMaxTzOffsetMinutes {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid tz_offset_minutes")
		return
	}

	job, err := service.CreateUsageExport(c.GetInt("id"), c.GetInt("role"), service.UsageExportRequest{
		View:            service.UsageView(request.View),
		OrganizationId:  request.OrganizationId,
		UserId:          request.UserId,
		StartTimestamp:  request.StartTimestamp,
		EndTimestamp:    request.EndTimestamp,
		TzOffsetMinutes: request.TzOffsetMinutes,
		Format:          request.Format,
		Kind:            request.Kind,
		WithBom:         request.WithBom,
	})
	switch {
	case errors.Is(err, service.ErrUsageExportKind):
		writeUsageError(c, http.StatusBadRequest, "USAGE_EXPORT_KIND_INVALID", "Invalid export format or kind")
		return
	case errors.Is(err, service.ErrUsageExportTooLarge):
		writeUsageError(c, http.StatusBadRequest, "USAGE_EXPORT_TOO_LARGE", "The export exceeds the row limit; narrow the range or the scope, or use CSV")
		return
	case errors.Is(err, service.ErrUsageExportBusy):
		writeUsageError(c, http.StatusTooManyRequests, "USAGE_EXPORT_BUSY", "Wait for your running exports to finish before starting another")
		return
	case err != nil:
		writeUsageScopeError(c, err)
		return
	}
	common.ApiSuccess(c, usageExportResponse(job, common.GetTimestamp()))
}

// ListUsageExports returns the caller's newest jobs.
func ListUsageExports(c *gin.Context) {
	jobs, err := model.ListUsageExportJobs(c.GetInt("id"), usageExportListLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	items := make([]gin.H, 0, len(jobs))
	for i := range jobs {
		items = append(items, usageExportResponse(&jobs[i], now))
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// loadOwnedUsageExport returns the job when the caller created it. Another user's job is
// refused, and an unknown job is not found.
func loadOwnedUsageExport(c *gin.Context) (*model.UsageExportJob, bool) {
	job, err := model.GetUsageExportJob(c.Param("job_id"))
	if errors.Is(err, model.ErrUsageExportNotFound) {
		writeUsageError(c, http.StatusNotFound, "USAGE_EXPORT_NOT_FOUND", "Export not found")
		return nil, false
	}
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if job.UserId != c.GetInt("id") {
		writeUsageError(c, http.StatusForbidden, "USAGE_EXPORT_FORBIDDEN", "This export belongs to another user")
		return nil, false
	}
	return job, true
}

// GetUsageExport returns one of the caller's jobs.
func GetUsageExport(c *gin.Context) {
	job, ok := loadOwnedUsageExport(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, usageExportResponse(job, common.GetTimestamp()))
}

// DownloadUsageExport streams a finished file. It checks the owner, the caller's current
// scope, and the job state, and it does not reveal the storage key.
func DownloadUsageExport(c *gin.Context) {
	job, ok := loadOwnedUsageExport(c)
	if !ok {
		return
	}
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		writeUsageError(c, http.StatusForbidden, "USAGE_SCOPE_FORBIDDEN", "This usage view is not available")
		return
	}
	if _, err := service.ResolveUsageScope(user.Id, user.Role, service.UsageScopeRequest{
		View:           service.UsageView(job.View),
		OrganizationId: job.OrganizationId,
		UserId:         job.TargetUserId,
	}); err != nil {
		writeUsageScopeError(c, err)
		return
	}

	switch job.Status {
	case model.UsageExportStatusQueued, model.UsageExportStatusRunning:
		writeUsageError(c, http.StatusConflict, "USAGE_EXPORT_NOT_READY", "The export is still being generated")
		return
	case model.UsageExportStatusFailed:
		writeUsageError(c, http.StatusConflict, "USAGE_EXPORT_FAILED", "The export failed")
		return
	case model.UsageExportStatusExpired:
		writeUsageError(c, http.StatusGone, "USAGE_EXPORT_EXPIRED", "The export has expired")
		return
	}
	if job.ExpiresTime <= common.GetTimestamp() {
		writeUsageError(c, http.StatusGone, "USAGE_EXPORT_EXPIRED", "The export has expired")
		return
	}
	file, err := service.OpenUsageExportFile(job)
	if err != nil {
		writeUsageError(c, http.StatusGone, "USAGE_EXPORT_EXPIRED", "The export has expired")
		return
	}
	defer file.Close()

	service.RecordUsageExportDownloaded(job)
	c.Header("Content-Type", service.UsageExportContentType(job.Format))

	c.Header("Content-Disposition", "attachment; filename=\""+job.FileName+"\"")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, file)
}
