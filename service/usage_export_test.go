package service

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// Fixture accounts match the usage report fixture: org A has an admin and a member, org B has
// one member, and user 13 belongs to no organization.
const (
	usageExportOrgAAdmin  = 10
	usageExportOrgAMember = 11
	usageExportOrgBMember = 12
	usageExportNoOrgUser  = 13
	usageExportRoot       = 14
	usageExportOrgA       = 1
	usageExportOrgB       = 2
)

func setupUsageExportServiceTest(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/usage-export.db?_pragma=busy_timeout(30000)&_txlock=immediate"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousStore := usageExportStore
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	SetUsageExportStore(NewLocalDiskExportStore(t.TempDir()))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		SetUsageExportStore(previousStore)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Organization{}, &model.OrganizationMembership{},
		&model.Log{}, &model.UsageExportJob{}, &model.AuditLog{}, &model.SystemTask{}))

	users := []model.User{
		{Id: usageExportOrgAAdmin, Username: "exp-org-a-admin", AffCode: "exp-aff-10", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageExportOrgAMember, Username: "exp-org-a-member", AffCode: "exp-aff-11", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageExportOrgBMember, Username: "exp-org-b-member", AffCode: "exp-aff-12", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageExportNoOrgUser, Username: "exp-no-org", AffCode: "exp-aff-13", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: usageExportRoot, Username: "exp-root", AffCode: "exp-aff-14", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
	}
	require.NoError(t, db.Create(&users).Error)
	require.NoError(t, db.Create(&model.Organization{Id: usageExportOrgA, Name: "Org A", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	require.NoError(t, db.Create(&model.Organization{Id: usageExportOrgB, Name: "Org B", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	memberships := []model.OrganizationMembership{
		{OrganizationId: usageExportOrgA, UserId: usageExportOrgAAdmin, Role: model.OrganizationRoleAdmin, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
		{OrganizationId: usageExportOrgA, UserId: usageExportOrgAMember, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
		{OrganizationId: usageExportOrgB, UserId: usageExportOrgBMember, Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1},
	}
	require.NoError(t, db.Create(&memberships).Error)

	base := time.Now().Unix() - 86400
	logs := []model.Log{
		{UserId: usageExportOrgAAdmin, Username: "org-a-admin", Type: model.LogTypeConsume, ModelName: "m1", Quota: 100, TokenName: "k1", RequestId: "a-admin", CreatedAt: base + 60, BillingOrganizationId: usageExportOrgA, ChannelId: 3, Ip: "10.1.1.1", UseTime: 2, Group: "default"},
		{UserId: usageExportOrgAMember, Username: "org-a-member", Type: model.LogTypeConsume, ModelName: "m1", Quota: 40, TokenName: "k2", RequestId: "a-member", CreatedAt: base + 120, BillingOrganizationId: usageExportOrgA, ChannelId: 3, Ip: "10.1.1.2"},
		{UserId: usageExportOrgAMember, Username: "org-a-member", Type: model.LogTypeConsume, ModelName: "m2", Quota: 15, TokenName: "k3", RequestId: "a-member-personal", CreatedAt: base + 180, BillingOrganizationId: 0, ChannelId: 4, Ip: "10.1.1.3"},
		{UserId: usageExportOrgBMember, Username: "org-b-member", Type: model.LogTypeRefund, ModelName: "m1", Quota: 70, TokenName: "k4", RequestId: "b-member", CreatedAt: base + 240, BillingOrganizationId: usageExportOrgB, ChannelId: 5, Ip: "10.2.2.2"},
		{UserId: usageExportNoOrgUser, Username: "=1+1", Type: model.LogTypeConsume, ModelName: "m1", Quota: 9, TokenName: "k5", RequestId: "personal", CreatedAt: base + 300, BillingOrganizationId: 0, ChannelId: 6, Ip: "10.3.3.3"},
	}
	require.NoError(t, db.Create(&logs).Error)
}

// usageExportTestWindow covers the fixture rows and nothing older than a week.
func usageExportTestWindow() (int64, int64) {
	now := time.Now().Unix()
	return now - 7*86400, now + 60
}

func usageExportCreate(t *testing.T, caller int, role int, request UsageExportRequest) *model.UsageExportJob {
	t.Helper()
	start, end := usageExportTestWindow()
	request.StartTimestamp, request.EndTimestamp = start, end
	job, err := CreateUsageExport(nil, caller, role, request)
	require.NoError(t, err)
	require.Equal(t, model.UsageExportStatusSucceeded, job.Status, "error code: %s", job.ErrorCode)
	return job
}

func usageExportReadFile(t *testing.T, job *model.UsageExportJob) []byte {
	t.Helper()
	reader, err := OpenUsageExportFile(job)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return data
}

func usageExportCSVRows(t *testing.T, data []byte) [][]string {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(string(data)))
	// The summary file holds sections of different widths, so the field count varies by row.
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	return rows
}

func TestUsageExportTextEscapesFormulaCharacters(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain text", "gpt-4o", "gpt-4o"},
		{"equals starts a formula", "=HYPERLINK(\"http://x\")", "'=HYPERLINK(\"http://x\")"},
		{"plus starts a formula", "+SUM(A1)", "'+SUM(A1)"},
		{"minus starts a formula", "-1", "'-1"},
		{"at starts a formula", "@cmd", "'@cmd"},
		{"tab starts a formula", "\tcmd", "'\tcmd"},
		{"carriage return starts a formula", "\rcmd", "'\rcmd"},
		{"formula character inside text is kept", "a=b", "a=b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, usageText(tc.input))
		})
	}
}

func TestUsageExportAuthorizesTheCallerWhenCreated(t *testing.T) {
	setupUsageExportServiceTest(t)
	start, end := usageExportTestWindow()
	base := UsageExportRequest{StartTimestamp: start, EndTimestamp: end, Format: UsageExportFormatCSV, Kind: UsageExportKindRecords}

	orgA := base
	orgA.View, orgA.OrganizationId = UsageViewOrganization, usageExportOrgA
	_, err := CreateUsageExport(nil, usageExportOrgAAdmin, common.RoleCommonUser, orgA)
	require.NoError(t, err, "an organization admin exports their own organization")

	_, err = CreateUsageExport(nil, usageExportOrgBMember, common.RoleCommonUser, orgA)
	require.Error(t, err, "a member of another organization cannot export this organization")
	assert.True(t, errors.Is(err, ErrUsageViewForbidden) || errors.Is(err, ErrUsageScopeMismatch), "got %v", err)

	otherUser := base
	otherUser.View, otherUser.UserId = UsageViewSelf, usageExportOrgAAdmin
	_, err = CreateUsageExport(nil, usageExportOrgAMember, common.RoleCommonUser, otherUser)
	require.Error(t, err, "a member cannot export another user's personal view")

	platform := base
	platform.View = UsageViewPlatform
	_, err = CreateUsageExport(nil, usageExportOrgAAdmin, common.RoleCommonUser, platform)
	require.Error(t, err, "the platform view needs a root or admin caller")

	bad := base
	bad.View, bad.Kind = UsageViewSelf, "bogus"
	_, err = CreateUsageExport(nil, usageExportNoOrgUser, common.RoleCommonUser, bad)
	assert.ErrorIs(t, err, ErrUsageExportKind)

	badFormat := base
	badFormat.View, badFormat.Format = UsageViewSelf, "pdf"
	_, err = CreateUsageExport(nil, usageExportNoOrgUser, common.RoleCommonUser, badFormat)
	assert.ErrorIs(t, err, ErrUsageExportKind)
}

func TestUsageExportCSVAndWorkbookReportTheSameNumbers(t *testing.T) {
	setupUsageExportServiceTest(t)
	summary := usageExportCreate(t, usageExportRoot, common.RoleRootUser, UsageExportRequest{
		View: UsageViewPlatform, Format: UsageExportFormatCSV, Kind: UsageExportKindSummary, WithBom: true})
	records := usageExportCreate(t, usageExportRoot, common.RoleRootUser, UsageExportRequest{
		View: UsageViewPlatform, Format: UsageExportFormatCSV, Kind: UsageExportKindRecords})
	workbook := usageExportCreate(t, usageExportRoot, common.RoleRootUser, UsageExportRequest{
		View: UsageViewPlatform, Format: UsageExportFormatXLSX})

	csvSummary := usageExportCSVRows(t, usageExportReadFile(t, summary))
	require.NotEmpty(t, csvSummary)
	assert.Equal(t, "section", strings.TrimPrefix(csvSummary[0][0], usageExportBOM), "the BOM precedes the header")

	book, err := excelize.OpenReader(strings.NewReader(string(usageExportReadFile(t, workbook))))
	require.NoError(t, err)
	defer book.Close()
	assert.Equal(t, []string{"汇总", "按模型", "按组织", "用量明细"}, book.GetSheetList())

	// 汇总: the workbook row equals the CSV summary row, cell for cell.
	sheet, err := book.GetRows("汇总")
	require.NoError(t, err)
	var csvTotals []string
	for _, row := range csvSummary {
		if len(row) > 0 && row[0] == "summary" {
			csvTotals = row[1:]
		}
	}
	require.Len(t, sheet, 2)
	assert.Equal(t, csvTotals, sheet[1])

	// 按模型: every model row matches, keyed by model name.
	csvModels := map[string][]string{}
	for _, row := range csvSummary {
		if len(row) > 0 && row[0] == "models" {
			csvModels[row[1]] = row[2:]
		}
	}
	models, err := book.GetRows("按模型")
	require.NoError(t, err)
	require.Len(t, models, len(csvModels)+1)
	for _, row := range models[1:] {
		assert.Equal(t, csvModels[row[0]], row[1:], "model %s", row[0])
	}

	// 用量明细: the workbook detail sheet has the same rows as the records CSV.
	csvRecords := usageExportCSVRows(t, usageExportReadFile(t, records))
	detail, err := book.GetRows("用量明细")
	require.NoError(t, err)
	assert.Equal(t, csvRecords, detail)
	assert.Equal(t, int64(len(csvRecords)-1), workbook.RowCount)

	// A formula-looking username is escaped the same way in both formats.
	var escaped bool
	for _, row := range detail {
		if len(row) > 2 && row[3] == "'=1+1" {
			escaped = true
		}
	}
	assert.True(t, escaped, "the username cell is escaped in the workbook")
}

func TestUsageExportAsyncRunMatchesInlineBytes(t *testing.T) {
	setupUsageExportServiceTest(t)
	inline := usageExportCreate(t, usageExportOrgAAdmin, common.RoleCommonUser, UsageExportRequest{
		View: UsageViewOrganization, OrganizationId: usageExportOrgA, Format: UsageExportFormatCSV,
		Kind: UsageExportKindRecords, WithBom: true})

	// The background path runs the same job body: a queued row, then RunUsageExportJob.
	start, end := usageExportTestWindow()
	queued := &model.UsageExportJob{
		JobId: common.GetUUID(), UserId: usageExportOrgAAdmin, View: string(UsageViewOrganization),
		OrganizationId: usageExportOrgA, StartTimestamp: start, EndTimestamp: end,
		Format: UsageExportFormatCSV, Kind: UsageExportKindRecords, WithBom: true,
		Status: model.UsageExportStatusQueued, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, model.CreateUsageExportJob(queued))
	require.NoError(t, RunUsageExportJob(queued.JobId))
	background, err := model.GetUsageExportJob(queued.JobId)
	require.NoError(t, err)
	require.Equal(t, model.UsageExportStatusSucceeded, background.Status)

	assert.Equal(t, usageExportReadFile(t, inline), usageExportReadFile(t, background))
	assert.Equal(t, inline.RowCount, background.RowCount)
	assert.True(t, strings.HasPrefix(string(usageExportReadFile(t, background)), usageExportBOM))
}

func TestUsageExportCleanupExpiresFilesAndFailsStaleJobs(t *testing.T) {
	setupUsageExportServiceTest(t)
	now := common.GetTimestamp()

	expired := &model.UsageExportJob{JobId: "expired-job", UserId: usageExportOrgAAdmin, View: string(UsageViewSelf),
		Format: UsageExportFormatCSV, Kind: UsageExportKindRecords, Status: model.UsageExportStatusSucceeded,
		StorageKey: "expired-job.csv", CreatedTime: now - 10, FinishedTime: now - 10, ExpiresTime: now - 1}
	live := &model.UsageExportJob{JobId: "live-job", UserId: usageExportOrgAAdmin, View: string(UsageViewSelf),
		Format: UsageExportFormatCSV, Kind: UsageExportKindRecords, Status: model.UsageExportStatusSucceeded,
		StorageKey: "live-job.csv", CreatedTime: now - 10, FinishedTime: now - 10, ExpiresTime: now + 3600}
	stale := &model.UsageExportJob{JobId: "stale-job", UserId: usageExportOrgAAdmin, View: string(UsageViewSelf),
		Format: UsageExportFormatCSV, Kind: UsageExportKindRecords, Status: model.UsageExportStatusRunning,
		CreatedTime: now - 2*usageExportStaleSeconds, StartedTime: now - 2*usageExportStaleSeconds}
	require.NoError(t, model.CreateUsageExportJob(expired))
	require.NoError(t, model.CreateUsageExportJob(live))
	require.NoError(t, model.CreateUsageExportJob(stale))
	_, err := usageExportStore.Put(expired.StorageKey, strings.NewReader("a,b\n"))
	require.NoError(t, err)
	_, err = usageExportStore.Put(live.StorageKey, strings.NewReader("a,b\n"))
	require.NoError(t, err)

	usageExportCleanupHandler{}.Run(context.Background(), &model.SystemTask{TaskID: "usage-export-cleanup-test"}, "runner")

	gone, err := model.GetUsageExportJob("expired-job")
	require.NoError(t, err)
	assert.Equal(t, model.UsageExportStatusExpired, gone.Status)
	_, err = usageExportStore.Open(expired.StorageKey)
	assert.Error(t, err, "the expired file is deleted")

	kept, err := model.GetUsageExportJob("live-job")
	require.NoError(t, err)
	assert.Equal(t, model.UsageExportStatusSucceeded, kept.Status)
	reader, err := usageExportStore.Open(live.StorageKey)
	require.NoError(t, err, "a live file is kept")
	require.NoError(t, reader.Close())

	failed, err := model.GetUsageExportJob("stale-job")
	require.NoError(t, err)
	assert.Equal(t, model.UsageExportStatusFailed, failed.Status)
	assert.Equal(t, "USAGE_EXPORT_STALE", failed.ErrorCode)
}

func TestUsageExportLimitsActiveJobsPerUser(t *testing.T) {
	setupUsageExportServiceTest(t)
	now := common.GetTimestamp()
	for i := 0; i < usageExportMaxActivePerUser; i++ {
		require.NoError(t, model.CreateUsageExportJob(&model.UsageExportJob{JobId: common.GetUUID(), UserId: usageExportNoOrgUser,
			View: string(UsageViewSelf), Format: UsageExportFormatCSV, Kind: UsageExportKindRecords,
			Status: model.UsageExportStatusQueued, CreatedTime: now}))
	}
	start, end := usageExportTestWindow()
	request := UsageExportRequest{View: UsageViewSelf, StartTimestamp: start, EndTimestamp: end, Format: UsageExportFormatCSV, Kind: UsageExportKindRecords}

	_, err := CreateUsageExport(nil, usageExportNoOrgUser, common.RoleCommonUser, request)
	assert.ErrorIs(t, err, ErrUsageExportBusy)

	job, err := CreateUsageExport(nil, usageExportOrgAAdmin, common.RoleCommonUser, request)
	require.NoError(t, err, "the limit is per user")
	assert.Equal(t, model.UsageExportStatusSucceeded, job.Status)
}
