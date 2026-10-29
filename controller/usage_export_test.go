package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUsageExportEndpoints adds the export routes to the usage report fixture. The fixture
// users and their own export directory are created here.
func setupUsageExportEndpoints(t *testing.T) *gin.Engine {
	t.Helper()
	engine := setupUsageReportEndpoints(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.UsageExportJob{}, &model.AuditLog{}))
	users := []model.User{
		{Id: usageOrgAAdmin, Username: "usage-export-org-a-admin", AffCode: "usage-export-aff-10", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageOrgAMember, Username: "usage-export-org-a-member", AffCode: "usage-export-aff-11", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageOrgBMember, Username: "usage-export-org-b-member", AffCode: "usage-export-aff-12", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageNoOrgUser, Username: "usage-export-no-org", AffCode: "usage-export-aff-13", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageRootUser, Username: "usage-export-root", AffCode: "usage-export-aff-14", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
	}
	require.NoError(t, model.DB.Create(&users).Error)
	// The audit trail resolves usernames through the user cache. Without Redis the lookup falls
	// back to the database, which is the path this fixture exercises.
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedis })
	previousStore := service.SetUsageExportStore(service.NewLocalDiskExportStore(t.TempDir()))
	t.Cleanup(func() { service.SetUsageExportStore(previousStore) })

	engine.POST("/api/usage/exports", CreateUsageExport)
	engine.GET("/api/usage/exports", ListUsageExports)
	engine.GET("/api/usage/exports/:job_id", GetUsageExport)
	engine.GET("/api/usage/exports/:job_id/download", DownloadUsageExport)
	return engine
}

// usageExportCall sends a request as the given user and role. A body, when given, is JSON.
func usageExportCall(t *testing.T, engine *gin.Engine, user int, role int, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload io.Reader
	if body != nil {
		data, err := common.Marshal(body)
		require.NoError(t, err)
		payload = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, payload)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-User", strconv.Itoa(user))
	request.Header.Set("X-Test-Role", strconv.Itoa(role))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func usageExportBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

// usageExportCreate creates an export as the caller and returns the job the response reports.
func usageExportCreate(t *testing.T, engine *gin.Engine, user int, role int, request map[string]any) map[string]any {
	t.Helper()
	created := usageExportCall(t, engine, user, role, http.MethodPost, "/api/usage/exports", request)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	return usageData(usageExportBody(t, created))
}

func TestUsageExportOwnerCreatesAndDownloadsItsFile(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "records", "format": "csv", "with_bom": true,
	})
	require.Equal(t, "succeeded", job["status"])
	jobId := job["job_id"].(string)
	assert.Equal(t, "/api/usage/exports/"+jobId+"/download", job["download_url"])

	download := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/"+jobId+"/download", nil)
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", download.Header().Get("Content-Type"))
	assert.Contains(t, download.Header().Get("Content-Disposition"), "usage-records-"+jobId+".csv")
	body := download.Body.String()
	assert.True(t, strings.HasPrefix(body, "\xEF\xBB\xBF"), "the BOM was requested")
	assert.Contains(t, body, "org-a-member")
	assert.NotContains(t, body, "org-b-member", "the file holds only the organization's rows")

	// The created and downloaded events are audited with the request's client address.
	for _, action := range []string{"usage_export.created", "usage_export.downloaded"} {
		var entry model.AuditLog
		require.NoError(t, model.DB.Where("action = ? AND user_id = ?", action, usageOrgAAdmin).First(&entry).Error)
		assert.Equal(t, "192.0.2.1", entry.Ip, action)
	}
}

func TestUsageExportResponsesNeverRevealStorageKeys(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	created := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodPost, "/api/usage/exports", map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "summary", "format": "csv",
	})
	require.Equal(t, http.StatusOK, created.Code)
	jobId := usageData(usageExportBody(t, created))["job_id"].(string)
	// file_name is the public download name; the storage key is the internal path and must not appear.
	assert.NotContains(t, created.Body.String(), "storage_key")
	assert.NotContains(t, created.Body.String(), "\"storage_path\"")

	listed := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports", nil)
	require.Equal(t, http.StatusOK, listed.Code)
	assert.Contains(t, listed.Body.String(), jobId)
	assert.NotContains(t, listed.Body.String(), "storage_key")
}

func TestUsageExportForeignJobIsRefused(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "records", "format": "csv",
	})
	jobId := job["job_id"].(string)

	for _, caller := range []struct {
		name string
		id   int
		role int
	}{
		{"organization member of the same organization", usageOrgAMember, common.RoleCommonUser},
		{"root", usageRootUser, common.RoleRootUser},
	} {
		t.Run(caller.name, func(t *testing.T) {
			get := usageExportCall(t, engine, caller.id, caller.role, http.MethodGet, "/api/usage/exports/"+jobId, nil)
			assert.Equal(t, http.StatusForbidden, get.Code)
			download := usageExportCall(t, engine, caller.id, caller.role, http.MethodGet, "/api/usage/exports/"+jobId+"/download", nil)
			assert.Equal(t, http.StatusForbidden, download.Code)
			assert.NotContains(t, download.Body.String(), "org-a-member", "no file content leaks")
		})
	}

	missing := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/no-such-job", nil)
	assert.Equal(t, http.StatusNotFound, missing.Code)
}

func TestUsageExportCreateRefusesAnotherOrganization(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	refused := usageExportCall(t, engine, usageOrgBMember, common.RoleCommonUser, http.MethodPost, "/api/usage/exports", map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "records", "format": "csv",
	})
	assert.Equal(t, http.StatusForbidden, refused.Code, refused.Body.String())
}

func TestUsageExportDownloadRechecksTheCallersCurrentScope(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "records", "format": "csv",
	})
	jobId := job["job_id"].(string)

	// The admin is demoted after the export was made. The file was authorized for an admin, so
	// the download must check the current role and refuse.
	require.NoError(t, model.DB.Model(&model.OrganizationMembership{}).
		Where("organization_id = ? AND user_id = ?", usageOrgA, usageOrgAAdmin).
		Update("role", model.OrganizationRoleMember).Error)

	download := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/"+jobId+"/download", nil)
	assert.Equal(t, http.StatusForbidden, download.Code, download.Body.String())
}

func TestUsageExportExpiredFileIsGone(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "records", "format": "csv",
	})
	jobId := job["job_id"].(string)
	require.NoError(t, model.DB.Model(&model.UsageExportJob{}).Where("job_id = ?", jobId).
		Update("expires_time", common.GetTimestamp()-1).Error)

	download := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/"+jobId+"/download", nil)
	assert.Equal(t, http.StatusGone, download.Code)
	assert.Contains(t, download.Body.String(), "USAGE_EXPORT_EXPIRED")

	got := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/"+jobId, nil)
	require.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, "expired", usageData(usageExportBody(t, got))["status"], "a job past its expiry reads as expired at once")
}

func TestUsageExportWorkbookDownloadIsAnXLSXFile(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "format": "xlsx",
	})
	require.Equal(t, "xlsx", job["format"])
	jobId := job["job_id"].(string)

	download := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports/"+jobId+"/download", nil)
	require.Equal(t, http.StatusOK, download.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", download.Header().Get("Content-Type"))
	assert.Contains(t, download.Header().Get("Content-Disposition"), ".xlsx")
	assert.True(t, strings.HasPrefix(download.Body.String(), "PK"), "an XLSX file is a zip archive")
}

func TestUsageExportListShowsOnlyTheCallersJobs(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	usageExportCreate(t, engine, usageOrgAAdmin, common.RoleCommonUser, map[string]any{
		"view": "organization", "organization_id": usageOrgA, "kind": "summary", "format": "csv",
	})

	own := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, "/api/usage/exports", nil)
	require.Equal(t, http.StatusOK, own.Code)
	items, _ := usageData(usageExportBody(t, own))["items"].([]any)
	assert.Len(t, items, 1)

	other := usageExportCall(t, engine, usageOrgAMember, common.RoleCommonUser, http.MethodGet, "/api/usage/exports", nil)
	require.Equal(t, http.StatusOK, other.Code)
	items, _ = usageData(usageExportBody(t, other))["items"].([]any)
	assert.Empty(t, items)
}
