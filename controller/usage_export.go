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
	View            string `json:"view" form:"view"`
	OrganizationId  int    `json:"organization_id" form:"organization_id"`
	UserId          int    `json:"user_id" form:"user_id"`
	ModelName       string `json:"model_name" form:"model_name"`
	StartTimestamp  int64  `json:"start_timestamp" form:"start_timestamp"`
	EndTimestamp    int64  `json:"end_timestamp" form:"end_timestamp"`
	TzOffsetMinutes int    `json:"tz_offset_minutes" form:"tz_offset_minutes"`
	Kind            string `json:"kind" form:"kind"`
	Format          string `json:"format" form:"format"`
	WithBom         bool   `json:"with_bom" form:"with_bom"`
}

// usageExportRequestFrom applies the window and option checks shared by every export endpoint,
// and returns the service request. It writes the error response and returns false when the
// request is not valid.
func usageExportRequestFrom(c *gin.Context, request usageExportCreateRequest) (service.UsageExportRequest, bool) {
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
		return service.UsageExportRequest{}, false
	}
	if request.TzOffsetMinutes < -usageMaxTzOffsetMinutes || request.TzOffsetMinutes > usageMaxTzOffsetMinutes {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid tz_offset_minutes")
		return service.UsageExportRequest{}, false
	}
	modelName, validModel := usageModelName(request.ModelName)
	if !validModel {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid model_name")
		return service.UsageExportRequest{}, false
	}
	return service.UsageExportRequest{
		View:            service.UsageView(request.View),
		OrganizationId:  request.OrganizationId,
		UserId:          request.UserId,
		ModelName:       modelName,
		StartTimestamp:  request.StartTimestamp,
		EndTimestamp:    request.EndTimestamp,
		TzOffsetMinutes: request.TzOffsetMinutes,
		Format:          request.Format,
		Kind:            request.Kind,
		WithBom:         request.WithBom,
	}, true
}

// writeUsageExportError maps the export service's refusals to responses. It returns false for an
// error it does not own, which the caller reports as a scope error.
func writeUsageExportError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, service.ErrUsageExportKind):
		writeUsageError(c, http.StatusBadRequest, "USAGE_EXPORT_KIND_INVALID", "Invalid export format or kind")
	case errors.Is(err, service.ErrUsageExportTooLarge):
		writeUsageError(c, http.StatusBadRequest, "USAGE_EXPORT_TOO_LARGE", "The export exceeds the row limit; narrow the range or the scope, or use CSV")
	case errors.Is(err, service.ErrUsageExportBusy):
		writeUsageError(c, http.StatusTooManyRequests, "USAGE_EXPORT_BUSY", "Wait for your running exports to finish before starting another")
	case errors.Is(err, service.ErrUsageGenerateBusy):
		writeUsageError(c, http.StatusTooManyRequests, "USAGE_EXPORT_GENERATE_BUSY", "Exports are being generated; try again shortly")
	case errors.Is(err, service.ErrUsageDownloadBusy):
		writeUsageError(c, http.StatusTooManyRequests, "USAGE_DOWNLOAD_BUSY", "Too many downloads are running; try again shortly")
	case errors.Is(err, service.ErrUsageExportQueueRequired):
		writeUsageError(c, http.StatusConflict, "USAGE_EXPORT_QUEUE_REQUIRED", "The export is too large to download directly; create a queued export instead")
	case errors.Is(err, service.ErrUsageExportUnavailable):
		writeUsageError(c, http.StatusServiceUnavailable, "USAGE_EXPORT_UNAVAILABLE", "Export limits are temporarily unavailable; try again shortly")
	default:
		return false
	}
	return true
}

// CreateUsageExport queues an export as a job. The file is generated in the background and kept
// for a day, so it can be downloaded more than once.
func CreateUsageExport(c *gin.Context) {
	var body usageExportCreateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid request")
		return
	}
	request, ok := usageExportRequestFrom(c, body)
	if !ok {
		return
	}
	job, err := service.CreateUsageExport(c, c.GetInt("id"), c.GetInt("role"), request)
	if err != nil && !writeUsageExportError(c, err) {
		writeUsageScopeError(c, err)
		return
	}
	if err != nil {
		return
	}
	common.ApiSuccess(c, usageExportResponse(job, common.GetTimestamp()))
}

// EstimateUsageExport reports whether an export would be delivered directly or queued as a job.
func EstimateUsageExport(c *gin.Context) {
	var query usageExportCreateRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid request")
		return
	}
	request, ok := usageExportRequestFrom(c, query)
	if !ok {
		return
	}
	estimate, err := service.EstimateUsageExport(c.GetInt("id"), c.GetInt("role"), request)
	if err != nil && !writeUsageExportError(c, err) {
		writeUsageScopeError(c, err)
		return
	}
	if err != nil {
		return
	}
	common.ApiSuccess(c, gin.H{"delivery": estimate.Delivery, "row_count": estimate.RowCount})
}

// DirectUsageExport streams a small export in the response. Nothing is stored. The download is
// limited to the download slots and bandwidth shared by all downloads.
func DirectUsageExport(c *gin.Context) {
	var query usageExportCreateRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		writeUsageError(c, http.StatusBadRequest, "USAGE_INVALID_REQUEST", "Invalid request")
		return
	}
	request, ok := usageExportRequestFrom(c, query)
	if !ok {
		return
	}
	download, err := service.AcquireUsageDownload(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		if !writeUsageExportError(c, err) {
			common.ApiError(c, err)
		}
		return
	}
	defer download.Release()

	direct, err := service.PrepareDirectUsageExport(c.GetInt("id"), c.GetInt("role"), request)
	if err != nil {
		if !writeUsageExportError(c, err) {
			writeUsageScopeError(c, err)
		}
		return
	}
	defer direct.Close()

	service.RecordUsageDirectExport(c, direct)
	c.Header("Content-Type", direct.ContentType())
	c.Header("Content-Disposition", "attachment; filename=\""+direct.FileName()+"\"")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if _, err := direct.Write(service.UsageDownloadWriter(c.Request.Context(), c.Writer)); err != nil {
		common.SysError("direct usage export stopped: " + err.Error())
	}
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
		"model_name":        job.ModelName,
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
	download, err := service.AcquireUsageDownload(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		if !writeUsageExportError(c, err) {
			common.ApiError(c, err)
		}
		return
	}
	defer download.Release()

	file, err := service.OpenUsageExportFile(job)
	if err != nil {
		writeUsageError(c, http.StatusGone, "USAGE_EXPORT_EXPIRED", "The export has expired")
		return
	}
	defer file.Close()

	service.RecordUsageExportDownloaded(c, job)
	c.Header("Content-Type", service.UsageExportContentType(job.Format))

	c.Header("Content-Disposition", "attachment; filename=\""+job.FileName+"\"")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = io.Copy(service.UsageDownloadWriter(c.Request.Context(), c.Writer), file)
}
