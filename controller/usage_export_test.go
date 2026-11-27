package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// setupUsageExportEndpoints adds the export routes to the usage report fixture. The fixture
// users and their own export directory are created here.
func setupUsageExportEndpoints(t *testing.T) *gin.Engine {
	t.Helper()
	return seedUsageExportEndpoints(t, setupUsageReportEndpoints(t))
}

func seedUsageExportEndpoints(t *testing.T, engine *gin.Engine) *gin.Engine {
	t.Helper()
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
	engine.GET("/api/usage/exports/estimate", EstimateUsageExport)
	engine.GET("/api/usage/exports/direct", DirectUsageExport)
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

// usageExportCreate queues an export as the caller and waits until the background job finishes.
// It returns the job as the API reports it.
func usageExportCreate(t *testing.T, engine *gin.Engine, user int, role int, request map[string]any) map[string]any {
	t.Helper()
	created := usageExportCall(t, engine, user, role, http.MethodPost, "/api/usage/exports", request)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	jobId := usageData(usageExportBody(t, created))["job_id"].(string)

	deadline := time.Now().Add(10 * time.Second)
	for {
		got := usageExportCall(t, engine, user, role, http.MethodGet, "/api/usage/exports/"+jobId, nil)
		require.Equal(t, http.StatusOK, got.Code, got.Body.String())
		job := usageData(usageExportBody(t, got))
		if job["status"] == "succeeded" || job["status"] == "failed" {
			return job
		}
		require.True(t, time.Now().Before(deadline), "job %s is still %s", jobId, job["status"])
		time.Sleep(5 * time.Millisecond)
	}
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
	// Keep the fixture alive until the asynchronous export has finished.
	require.Eventually(t, func() bool {
		job, err := model.GetUsageExportJob(jobId)
		return err == nil && (job.Status == model.UsageExportStatusSucceeded || job.Status == model.UsageExportStatusFailed)
	}, 10*time.Second, 5*time.Millisecond)
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

// An export keeps the model filter it was created with, so the file matches the report on screen.
func TestUsageExportCarriesTheModelFilter(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	job := usageExportCreate(t, engine, usageRootUser, common.RoleRootUser, map[string]any{
		"view": "platform", "model_name": "m2", "kind": "records", "format": "csv",
	})
	require.Equal(t, "succeeded", job["status"])
	assert.Equal(t, "m2", job["model_name"])
	assert.Equal(t, float64(1), job["row_count"], "only the m2 request of the fixture")

	download := usageExportCall(t, engine, usageRootUser, common.RoleRootUser, http.MethodGet, "/api/usage/exports/"+job["job_id"].(string)+"/download", nil)
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	assert.Contains(t, download.Body.String(), "a-member-personal")
	assert.NotContains(t, download.Body.String(), "a-admin", "m1 rows are excluded")
}

func TestUsageExportDirectDeliveryAppearsInHistoryWithoutAStoredFile(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			switch dialect {
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				db, _ = newAuditTestDatabase(t, dialect, dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.True(t, parsed.Hostname() == "127.0.0.1" && parsed.Port() == "5432" && parsed.Path == "/newapi_e2e", "use only the local PostgreSQL E2E database")
				base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
				require.NoError(t, err)
				t.Cleanup(func() { closeGormHandles(base) })
				db = base.Begin()
				require.NoError(t, db.Error)
				t.Cleanup(func() { assert.NoError(t, db.Rollback().Error) })
				schema := clause.Table{Name: "direct_export_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)}
				require.NoError(t, db.Exec("CREATE SCHEMA ?", schema).Error)
				require.NoError(t, db.Exec("SET LOCAL search_path TO ?", schema).Error)
			default:
				db, _ = newAuditTestDatabase(t, dialect, "")
			}
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			engine := seedUsageExportEndpoints(t, seedUsageReportEndpoints(t, db, common.DatabaseType(dialect)))
			verifyDirectUsageExportHistory(t, engine)
		})
	}
}

func verifyDirectUsageExportHistory(t *testing.T, engine *gin.Engine) {
	t.Helper()
	engine.GET("/api/usage/v2/exports", ListUsageExports)
	engine.GET("/api/usage/v2/exports/direct", DirectUsageExport)
	engine.GET("/api/usage/v2/exports/:job_id/download", DownloadUsageExport)
	query := "/api/usage/exports/estimate?view=organization&organization_id=1&kind=records&format=csv"
	estimated := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, query, nil)
	require.Equal(t, http.StatusOK, estimated.Code, estimated.Body.String())
	estimate := usageData(usageExportBody(t, estimated))
	assert.Equal(t, "direct", estimate["delivery"])
	assert.Equal(t, float64(2), estimate["row_count"], "organization A paid for two requests in the fixture")

	for _, format := range []string{"csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			const exports = "/api/usage/v2/exports"
			path := exports + "/direct?view=organization&payer_type=organization&organization_id=1&kind=summary&format=" + format
			direct := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, path, nil)
			require.Equal(t, http.StatusOK, direct.Code, direct.Body.String())
			assert.Equal(t, service.UsageExportContentType(format), direct.Header().Get("Content-Type"))
			assert.Contains(t, direct.Header().Get("Content-Disposition"), "attachment")

			status, body := usageCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, exports+"?include_direct=true")
			require.Equal(t, http.StatusOK, status)
			items := usageData(body)["items"].([]any)
			require.NotEmpty(t, items)
			row := items[0].(map[string]any)
			assert.Equal(t, "direct_downloaded", row["status"])
			assert.Equal(t, "summary", row["kind"])
			assert.Equal(t, format, row["format"])
			assert.Equal(t, "organization", row["payer_type"])
			assert.Equal(t, float64(4), row["row_count"], "total, model and two user rows")
			assert.Equal(t, float64(0), row["expires_time"])
			assert.NotContains(t, row, "download_url")
			id := row["job_id"].(string)
			stored, err := model.GetUsageExportJob(id)
			require.NoError(t, err)
			assert.Empty(t, stored.StorageKey)
			assert.Contains(t, direct.Header().Get("Content-Disposition"), stored.FileName)
			var event model.AuditLog
			require.NoError(t, model.DB.Where("action = ? AND user_id = ? AND content LIKE ?", "usage_export.direct_downloaded", usageOrgAAdmin, "%"+id+"%").First(&event).Error)

			undownloadable := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet, exports+"/"+id+"/download", nil)
			assert.Equal(t, http.StatusGone, undownloadable.Code)
			assert.Contains(t, undownloadable.Body.String(), "USAGE_EXPORT_NO_STORED_FILE")
		})
	}
	status, body := usageCall(t, engine, usageOrgAMember, common.RoleCommonUser, "/api/usage/v2/exports?include_direct=true")
	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, usageData(body)["items"], "another member never sees the owner's exports")
	status, body = usageCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, "/api/usage/v2/exports")
	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, usageData(body)["items"], "old clients do not receive a new status")

	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&model.UsageExportJob{
		JobId: "recent-direct", UserId: usageOrgAAdmin, Status: model.UsageExportStatusSucceeded, CreatedTime: now + 1,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UsageExportJob{
		JobId: "ready-export", UserId: usageOrgAAdmin, Status: model.UsageExportStatusSucceeded,
		StorageKey: "ready-export.csv", CreatedTime: now - 120, ExpiresTime: now + 60,
	}).Error)
	// Simulate the retention cutoff passing for the direct records, but not for a ready file.
	purged, err := model.DeleteUsageExportJobsCreatedBefore(now + 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, purged)
	status, body = usageCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, "/api/usage/v2/exports?include_direct=true")
	require.Equal(t, http.StatusOK, status)
	items := usageData(body)["items"].([]any)
	require.Len(t, items, 2)
	assert.Equal(t, "ready-export", items[0].(map[string]any)["job_id"])
	assert.Equal(t, "recent-direct", items[1].(map[string]any)["job_id"])
}

func TestUsageExportInterruptedDirectStreamDoesNotAppearInHistory(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	request := httptest.NewRequest(http.MethodGet, "/api/usage/exports/direct?kind=records&format=csv", nil)
	request.Header.Set("X-Test-User", strconv.Itoa(usageOrgAMember))
	request.Header.Set("X-Test-Role", strconv.Itoa(common.RoleCommonUser))
	writer := &usageExportDisconnectedWriter{ResponseRecorder: httptest.NewRecorder()}
	engine.ServeHTTP(writer, request)
	require.True(t, writer.attempted, "the exporter reached the response stream")

	status, body := usageCall(t, engine, usageOrgAMember, common.RoleCommonUser, "/api/usage/exports?include_direct=true")
	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, usageData(body)["items"])
}

type usageExportDisconnectedWriter struct {
	*httptest.ResponseRecorder
	attempted bool
}

func (w *usageExportDisconnectedWriter) Write([]byte) (int, error) {
	w.attempted = true
	return 0, io.ErrClosedPipe
}

// A user who holds their download share is refused, even when global download slots are free.
func TestUsageExportDownloadsAreLimitedPerUser(t *testing.T) {
	engine := setupUsageExportEndpoints(t)
	ctx := context.Background()
	first, err := service.AcquireUsageDownload(ctx, usageOrgAAdmin)
	require.NoError(t, err)
	defer first.Release()
	second, err := service.AcquireUsageDownload(ctx, usageOrgAAdmin)
	require.NoError(t, err)
	defer second.Release()

	refused := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet,
		"/api/usage/exports/direct?view=organization&organization_id=1&kind=records&format=csv", nil)
	assert.Equal(t, http.StatusTooManyRequests, refused.Code)
	assert.Contains(t, refused.Body.String(), "USAGE_DOWNLOAD_BUSY")

	second.Release()
	allowed := usageExportCall(t, engine, usageOrgAAdmin, common.RoleCommonUser, http.MethodGet,
		"/api/usage/exports/direct?view=organization&organization_id=1&kind=records&format=csv", nil)
	assert.Equal(t, http.StatusOK, allowed.Code)
}

// Each export limit answers with its own status and code, so the page can explain the refusal.
func TestUsageExportLimitErrorsHaveTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrUsageDownloadBusy, http.StatusTooManyRequests, "USAGE_DOWNLOAD_BUSY"},
		{service.ErrUsageGenerateBusy, http.StatusTooManyRequests, "USAGE_EXPORT_GENERATE_BUSY"},
		{service.ErrUsageExportQueueRequired, http.StatusConflict, "USAGE_EXPORT_QUEUE_REQUIRED"},
		{service.ErrUsageExportUnavailable, http.StatusServiceUnavailable, "USAGE_EXPORT_UNAVAILABLE"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		require.True(t, writeUsageExportError(c, tc.err), tc.code)
		assert.Equal(t, tc.status, recorder.Code, tc.code)
		assert.Contains(t, recorder.Body.String(), tc.code)
	}
}
