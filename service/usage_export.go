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

package service

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

const (
	// UsageExportKindRecords is the detail table as CSV.
	UsageExportKindRecords = "records"
	// UsageExportKindSummary is the summary, model and ranking tables as CSV.
	UsageExportKindSummary = "summary"
	// UsageExportKindWorkbook is the XLSX workbook: summary, model, ranking and detail sheets.
	UsageExportKindWorkbook = "workbook"

	UsageExportFormatCSV  = "csv"
	UsageExportFormatXLSX = "xlsx"

	usageExportTTLSeconds       = 7 * 24 * 3600
	usageExportSyncRowLimit     = 5000
	usageExportMaxRows          = 500000
	usageExportMaxXLSXRows      = 100000
	usageExportBatchSize        = 1000
	usageExportMaxActivePerUser = 3
	usageExportStaleSeconds     = 30 * 60
	usageExportDefaultDir       = "data/usage-exports"
	usageExportDirEnv           = "USAGE_EXPORT_DIR"
	usageExportBOM              = "\xEF\xBB\xBF"
	usageExportTimeLayout       = "2006-01-02 15:04:05"
	usageExportContentCSV       = "text/csv; charset=utf-8"
	usageExportContentXLSX      = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	usageExportErrorScope       = "USAGE_SCOPE_FORBIDDEN"
	usageExportErrorTooLarge    = "USAGE_EXPORT_TOO_LARGE"
	usageExportErrorFailed      = "USAGE_EXPORT_FAILED"
)

var (
	ErrUsageExportKind     = errors.New("invalid export format or kind")
	ErrUsageExportTooLarge = errors.New("export exceeds the row limit")
	ErrUsageExportBusy     = errors.New("too many exports are still running")
)

// usageExportStore holds generated files. Set it at startup in tests or deployments that
// need another directory.
var usageExportStore ExportStore = NewLocalDiskExportStore(usageExportDirectory())

func usageExportDirectory() string {
	if dir := os.Getenv(usageExportDirEnv); dir != "" {
		return dir
	}
	return usageExportDefaultDir
}

// SetUsageExportStore replaces the store that holds export files and returns the previous one.
// Call it before the server starts serving; tests use it to write files into a temporary directory.
func SetUsageExportStore(store ExportStore) ExportStore {
	previous := usageExportStore
	usageExportStore = store
	return previous
}

// UsageExportRequest is what the caller asked for. Scope fields are intent only; the export
// is authorized again against the caller's current role each time it runs.
type UsageExportRequest struct {
	View            UsageView
	OrganizationId  int
	UserId          int
	StartTimestamp  int64
	EndTimestamp    int64
	TzOffsetMinutes int
	Format          string
	Kind            string
	WithBom         bool
}

// CreateUsageExport authorizes the request now and stores a job. Small exports finish inside
// the call; large ones run in the background and the caller polls the job.
func CreateUsageExport(c *gin.Context, callerId int, callerRole int, request UsageExportRequest) (*model.UsageExportJob, error) {
	format := request.Format
	if format == "" {
		format = UsageExportFormatCSV
	}
	kind, err := usageExportKind(format, request.Kind)
	if err != nil {
		return nil, err
	}
	resolved, err := ResolveUsageScope(callerId, callerRole, UsageScopeRequest{
		View:           request.View,
		OrganizationId: request.OrganizationId,
		UserId:         request.UserId,
	})
	if err != nil {
		return nil, err
	}
	window := usageExportWindow(request.StartTimestamp, request.EndTimestamp, request.TzOffsetMinutes)
	active, err := model.CountActiveUsageExportJobs(callerId)
	if err != nil {
		return nil, err
	}
	if active >= usageExportMaxActivePerUser {
		return nil, ErrUsageExportBusy
	}
	inline := true
	if kind == UsageExportKindRecords || kind == UsageExportKindWorkbook {
		count, err := model.CountUsageRecords(resolved.Scope, window)
		if err != nil {
			return nil, err
		}
		if count > usageExportRowLimit(format) {
			return nil, ErrUsageExportTooLarge
		}
		inline = count <= usageExportSyncRowLimit
	}

	job := &model.UsageExportJob{
		JobId:           common.GetUUID(),
		UserId:          callerId,
		View:            string(resolved.View),
		OrganizationId:  resolved.Scope.OrganizationId,
		TargetUserId:    resolved.Scope.UserId,
		StartTimestamp:  request.StartTimestamp,
		EndTimestamp:    request.EndTimestamp,
		TzOffsetMinutes: request.TzOffsetMinutes,
		Format:          format,
		Kind:            kind,
		WithBom:         request.WithBom && format == UsageExportFormatCSV,
		Status:          model.UsageExportStatusQueued,
		CreatedTime:     common.GetTimestamp(),
	}
	if err := model.CreateUsageExportJob(job); err != nil {
		return nil, err
	}
	recordUsageExportEvent(c, job, "created", "")

	if inline {
		if err := RunUsageExportJob(job.JobId); err != nil {
			common.SysError("usage export " + job.JobId + " failed: " + err.Error())
		}
		return model.GetUsageExportJob(job.JobId)
	}
	jobId := job.JobId
	gopool.Go(func() {
		if err := RunUsageExportJob(jobId); err != nil {
			common.SysError("usage export " + jobId + " failed: " + err.Error())
		}
	})
	return job, nil
}

// usageExportKind returns the stored kind for a request. XLSX is always the whole workbook;
// CSV is either the detail table or the summary tables.
func usageExportKind(format string, kind string) (string, error) {
	switch format {
	case UsageExportFormatXLSX:
		return UsageExportKindWorkbook, nil
	case UsageExportFormatCSV:
		if kind == UsageExportKindRecords || kind == UsageExportKindSummary {
			return kind, nil
		}
	}
	return "", ErrUsageExportKind
}

func usageExportRowLimit(format string) int64 {
	if format == UsageExportFormatXLSX {
		return usageExportMaxXLSXRows
	}
	return usageExportMaxRows
}

// UsageExportContentType is the media type a format is served with.
func UsageExportContentType(format string) string {
	if format == UsageExportFormatXLSX {
		return usageExportContentXLSX
	}
	return usageExportContentCSV
}

// RunUsageExportJob generates one queued job. It claims the job first, so a job runs once.
func RunUsageExportJob(jobId string) error {
	claimed, err := model.ClaimUsageExportJob(jobId, common.GetTimestamp())
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	job, err := model.GetUsageExportJob(jobId)
	if err != nil {
		return err
	}
	rows, key, fileName, err := generateUsageExport(job)
	if err != nil {
		code := usageExportErrorCode(err)
		now := common.GetTimestamp()
		if finishErr := model.FinishUsageExportJob(jobId, model.UsageExportStatusFailed, 0, "", "", code, now, 0); finishErr != nil {
			return finishErr
		}
		recordUsageExportEvent(nil, job, "failed", " error="+code)
		return err
	}
	now := common.GetTimestamp()
	if err := model.FinishUsageExportJob(jobId, model.UsageExportStatusSucceeded, rows, fileName, key, "", now, now+usageExportTTLSeconds); err != nil {
		return err
	}
	recordUsageExportEvent(nil, job, "succeeded", fmt.Sprintf(" rows=%d", rows))
	return nil
}

// generateUsageExport re-authorizes the job for its owner's current role, then streams the
// file into the store. The writer and the store run concurrently through a pipe, so memory
// stays bounded by the query batches, not by the export size.
func generateUsageExport(job *model.UsageExportJob) (int64, string, string, error) {
	user, err := model.GetUserById(job.UserId, false)
	if err != nil {
		return 0, "", "", ErrUsageViewForbidden
	}
	resolved, err := ResolveUsageScope(job.UserId, user.Role, UsageScopeRequest{
		View:           UsageView(job.View),
		OrganizationId: job.OrganizationId,
		UserId:         job.TargetUserId,
	})
	if err != nil {
		return 0, "", "", err
	}
	window := usageExportWindow(job.StartTimestamp, job.EndTimestamp, job.TzOffsetMinutes)
	key := job.JobId + "." + job.Format
	fileName := fmt.Sprintf("usage-%s-%s.%s", job.Kind, job.JobId, job.Format)

	reader, writer := io.Pipe()
	var rows int64
	var writeErr error
	done := make(chan struct{})
	gopool.Go(func() {
		defer close(done)
		rows, writeErr = writeUsageExport(writer, job, resolved, window)
		_ = writer.CloseWithError(writeErr)
	})
	_, putErr := usageExportStore.Put(key, reader)
	_ = reader.Close()
	<-done
	if writeErr != nil {
		_ = usageExportStore.Delete(key)
		return 0, "", "", writeErr
	}
	if putErr != nil {
		return 0, "", "", putErr
	}
	return rows, key, fileName, nil
}

// writeUsageExport writes the job's tables in its format. Both formats read the same queries,
// so a CSV and an XLSX of the same request carry the same numbers.
func writeUsageExport(w io.Writer, job *model.UsageExportJob, resolved ResolvedUsageScope, window model.UsageWindow) (int64, error) {
	names := newUsageOrganizationNames()
	if job.Format == UsageExportFormatXLSX {
		out := newUsageWorkbookWriter(w)
		rows, err := writeUsageTables(out, job, resolved, window, names)
		if err != nil {
			return rows, err
		}
		return rows, out.Close()
	}
	if job.WithBom {
		if _, err := io.WriteString(w, usageExportBOM); err != nil {
			return 0, err
		}
	}
	out := newUsageCSVWriter(w, job.Kind == UsageExportKindSummary)
	rows, err := writeUsageTables(out, job, resolved, window, names)
	if err != nil {
		return rows, err
	}
	return rows, out.Close()
}

// usageSheetWriter receives tables one after another. The CSV writer puts them in one file,
// separated by blank rows; the workbook writer puts each on its own sheet.
type usageSheetWriter interface {
	StartTable(id string, title string, header []usageCell) error
	WriteRow(cells []usageCell) error
	Close() error
}

// usageCell is one value in an export row. A number keeps its integer value, so the workbook
// stores it as a number; its text form is what the CSV writes.
type usageCell struct {
	text     string
	number   int64
	isNumber bool
}

func usageHeader(names ...string) []usageCell {
	cells := make([]usageCell, len(names))
	for i, name := range names {
		cells[i] = usageCell{text: name}
	}
	return cells
}

func usageTextCell(value string) usageCell {
	return usageCell{text: usageText(value)}
}

func usageNumberCell(value int64) usageCell {
	return usageCell{text: strconv.FormatInt(value, 10), number: value, isNumber: true}
}

func usageBoolCell(value bool) usageCell {
	return usageCell{text: strconv.FormatBool(value)}
}

func usageCellTexts(cells []usageCell) []string {
	texts := make([]string, len(cells))
	for i, cell := range cells {
		texts[i] = cell.text
	}
	return texts
}

func usageWorkbookValues(cells []usageCell) []any {
	values := make([]any, len(cells))
	for i, cell := range cells {
		if cell.isNumber {
			values[i] = cell.number
		} else {
			values[i] = cell.text
		}
	}
	return values
}

type usageCSVWriter struct {
	csv       *csv.Writer
	sectioned bool
	started   bool
	section   string
}

// newUsageCSVWriter writes UTF-8 CSV. A sectioned file prefixes every row with its table id,
// so the summary tables can share one file.
func newUsageCSVWriter(w io.Writer, sectioned bool) *usageCSVWriter {
	return &usageCSVWriter{csv: csv.NewWriter(w), sectioned: sectioned}
}

func (w *usageCSVWriter) StartTable(id string, _ string, header []usageCell) error {
	if w.started {
		if err := w.csv.Write([]string{}); err != nil {
			return err
		}
	}
	w.started = true
	w.section = id
	names := usageCellTexts(header)
	if w.sectioned {
		names = append([]string{"section"}, names...)
	}
	return w.csv.Write(names)
}

func (w *usageCSVWriter) WriteRow(cells []usageCell) error {
	texts := usageCellTexts(cells)
	if w.sectioned {
		texts = append([]string{w.section}, texts...)
	}
	return w.csv.Write(texts)
}

func (w *usageCSVWriter) Close() error {
	w.csv.Flush()
	return w.csv.Error()
}

// usageWorkbookWriter writes one sheet per table through excelize's stream writer, so a sheet's
// rows are not held in memory as cells. The file is assembled and written at Close.
type usageWorkbookWriter struct {
	out    io.Writer
	file   *excelize.File
	stream *excelize.StreamWriter
	sheets int
	row    int
}

func newUsageWorkbookWriter(out io.Writer) *usageWorkbookWriter {
	return &usageWorkbookWriter{out: out, file: excelize.NewFile()}
}

func (w *usageWorkbookWriter) StartTable(_ string, title string, header []usageCell) error {
	if err := w.closeSheet(); err != nil {
		return err
	}
	if w.sheets == 0 {
		if err := w.file.SetSheetName("Sheet1", title); err != nil {
			return err
		}
	} else if _, err := w.file.NewSheet(title); err != nil {
		return err
	}
	w.sheets++
	stream, err := w.file.NewStreamWriter(title)
	if err != nil {
		return err
	}
	w.stream = stream
	w.row = 0
	return w.WriteRow(header)
}

func (w *usageWorkbookWriter) WriteRow(cells []usageCell) error {
	w.row++
	reference, err := excelize.CoordinatesToCellName(1, w.row)
	if err != nil {
		return err
	}
	return w.stream.SetRow(reference, usageWorkbookValues(cells))
}

func (w *usageWorkbookWriter) closeSheet() error {
	if w.stream == nil {
		return nil
	}
	err := w.stream.Flush()
	w.stream = nil
	return err
}

func (w *usageWorkbookWriter) Close() error {
	if err := w.closeSheet(); err != nil {
		return err
	}
	if err := w.file.Write(w.out); err != nil {
		return err
	}
	return w.file.Close()
}

// writeUsageTables writes the tables for the job's kind. It returns the row count the job
// reports: detail rows for records and workbook exports, summary rows for summary exports.
func writeUsageTables(out usageSheetWriter, job *model.UsageExportJob, resolved ResolvedUsageScope, window model.UsageWindow, names *usageOrganizationNames) (int64, error) {
	switch job.Kind {
	case UsageExportKindRecords:
		return writeUsageRecordTable(out, job, resolved, window, names)
	case UsageExportKindSummary:
		return writeUsageSummaryTables(out, resolved, window, names)
	case UsageExportKindWorkbook:
		if _, err := writeUsageSummaryTables(out, resolved, window, names); err != nil {
			return 0, err
		}
		return writeUsageRecordTable(out, job, resolved, window, names)
	default:
		return 0, ErrUsageExportKind
	}
}

// usageRecordColumns lists the record fields for a view. The fields match the record shapes
// the report endpoints return to the same audience.
func usageRecordColumns(view UsageView) []string {
	columns := []string{"id", "created_at", "type", "username", "token_name", "model_name",
		"quota_platform_units", "prompt_tokens", "completion_tokens", "use_time", "is_stream", "request_id"}
	switch view {
	case UsageViewPlatform:
		return append(columns, "user_id", "token_id", "group", "channel_id", "ip", "billing_organization_id")
	case UsageViewOrganization:
		return columns
	default:
		return append(columns, "billing_organization_id", "billing_organization_name")
	}
}

func writeUsageRecordTable(out usageSheetWriter, job *model.UsageExportJob, resolved ResolvedUsageScope, window model.UsageWindow, names *usageOrganizationNames) (int64, error) {
	if err := out.StartTable("records", "用量明细", usageHeader(usageRecordColumns(resolved.View)...)); err != nil {
		return 0, err
	}
	limit := usageExportRowLimit(job.Format)
	var rows int64
	err := model.ForEachUsageRecord(resolved.Scope, window, usageExportBatchSize, func(log model.Log) error {
		rows++
		if rows > limit {
			return ErrUsageExportTooLarge
		}
		return out.WriteRow(usageRecordCells(resolved.View, log, job.TzOffsetMinutes, names))
	})
	return rows, err
}

func usageRecordCells(view UsageView, log model.Log, tzOffsetMinutes int, names *usageOrganizationNames) []usageCell {
	cells := []usageCell{
		usageNumberCell(int64(log.Id)),
		usageTextCell(usageExportTime(log.CreatedAt, tzOffsetMinutes)),
		usageTextCell(usageLogTypeLabel(log.Type)),
		usageTextCell(log.Username),
		usageTextCell(log.TokenName),
		usageTextCell(log.ModelName),
		usageNumberCell(int64(log.Quota)),
		usageNumberCell(int64(log.PromptTokens)),
		usageNumberCell(int64(log.CompletionTokens)),
		usageNumberCell(int64(log.UseTime)),
		usageBoolCell(log.IsStream),
		usageTextCell(log.RequestId),
	}
	switch view {
	case UsageViewPlatform:
		return append(cells,
			usageNumberCell(int64(log.UserId)), usageNumberCell(int64(log.TokenId)), usageTextCell(log.Group),
			usageNumberCell(int64(log.ChannelId)), usageTextCell(log.Ip), usageNumberCell(int64(log.BillingOrganizationId)))
	case UsageViewOrganization:
		return cells
	default:
		return append(cells,
			usageNumberCell(int64(log.BillingOrganizationId)), usageTextCell(names.nameOf(log.BillingOrganizationId)))
	}
}

// writeUsageSummaryTables writes the summary, model and ranking tables. The ranking table is
// written only when the view ranks something; a self view has no ranking.
func writeUsageSummaryTables(out usageSheetWriter, resolved ResolvedUsageScope, window model.UsageWindow, names *usageOrganizationNames) (int64, error) {
	var rows int64
	summary, err := model.GetUsageSummary(resolved.Scope, window)
	if err != nil {
		return 0, err
	}
	if err := out.StartTable("summary", "汇总", usageHeader("net_quota", "consume_quota", "refund_quota", "requests", "prompt_tokens", "completion_tokens")); err != nil {
		return 0, err
	}
	if err := out.WriteRow([]usageCell{usageNumberCell(summary.NetQuota), usageNumberCell(summary.ConsumeQuota),
		usageNumberCell(summary.RefundQuota), usageNumberCell(summary.Requests), usageNumberCell(summary.PromptTokens), usageNumberCell(summary.CompletionTokens)}); err != nil {
		return 0, err
	}
	rows++

	models, err := model.GetUsageModels(resolved.Scope, window)
	if err != nil {
		return 0, err
	}
	if err := out.StartTable("models", "按模型", usageHeader("model_name", "net_quota", "requests", "prompt_tokens", "completion_tokens")); err != nil {
		return 0, err
	}
	for _, row := range models {
		rows++
		if err := out.WriteRow([]usageCell{usageTextCell(row.ModelName), usageNumberCell(row.NetQuota),
			usageNumberCell(row.Requests), usageNumberCell(row.PromptTokens), usageNumberCell(row.CompletionTokens)}); err != nil {
			return 0, err
		}
	}

	by, ranked := UsageRankingFor(resolved)
	if !ranked {
		return rows, nil
	}
	ranking, err := model.GetUsageRanking(resolved.Scope, window, by)
	if err != nil {
		return 0, err
	}
	title := "按成员"
	if by == model.UsageRankByOrganization {
		title = "按组织"
	}
	if err := out.StartTable("rankings", title, usageHeader("key", "name", "net_quota", "requests")); err != nil {
		return 0, err
	}
	for _, row := range ranking {
		rows++
		name := usageTextCell(row.Name)
		if by == model.UsageRankByOrganization {
			name = usageTextCell(names.nameOf(int(row.Key)))
		}
		if err := out.WriteRow([]usageCell{usageNumberCell(row.Key), name,
			usageNumberCell(row.NetQuota), usageNumberCell(row.Requests)}); err != nil {
			return 0, err
		}
	}
	return rows, nil
}

// usageText makes a text cell safe for spreadsheets: a value that starts with a formula
// character is prefixed with an apostrophe, so it is shown as text rather than evaluated.
func usageText(value string) string {
	if value == "" {
		return ""
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

func usageLogTypeLabel(logType int) string {
	if logType == model.LogTypeRefund {
		return "refund"
	}
	return "consume"
}

func usageExportTime(timestamp int64, tzOffsetMinutes int) string {
	local := time.Unix(timestamp, 0).UTC().Add(time.Duration(tzOffsetMinutes) * time.Minute)
	return local.Format(usageExportTimeLayout)
}

func usageExportWindow(start int64, end int64, tzOffsetMinutes int) model.UsageWindow {
	return model.UsageWindow{Start: start, End: end, TzOffsetSeconds: int64(tzOffsetMinutes) * 60}
}

func usageExportErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrUsageViewForbidden), errors.Is(err, ErrUsageScopeMismatch):
		return usageExportErrorScope
	case errors.Is(err, ErrUsageExportTooLarge):
		return usageExportErrorTooLarge
	default:
		return usageExportErrorFailed
	}
}

// usageOrganizationNames caches organization names for one export, so each organization is
// read once.
type usageOrganizationNames struct {
	names map[int]string
}

func newUsageOrganizationNames() *usageOrganizationNames {
	return &usageOrganizationNames{names: map[int]string{}}
}

func (cache *usageOrganizationNames) nameOf(organizationId int) string {
	if organizationId <= 0 {
		return ""
	}
	if name, ok := cache.names[organizationId]; ok {
		return name
	}
	organization, err := model.GetOrganizationById(organizationId)
	name := ""
	if err == nil {
		name = organization.Name
	}
	cache.names[organizationId] = name
	return name
}

// recordUsageExportEvent writes an audit event for a job. Events carry the job id, the format
// and the outcome, never file content or keys. c is the request that caused the event, so the
// audit row carries its IP and user agent; background generation passes nil, because it has no
// request.
func recordUsageExportEvent(c *gin.Context, job *model.UsageExportJob, action string, detail string) {
	model.RecordAuditLog(c, model.AuditLog{
		UserId:   job.UserId,
		Category: model.AuditCategoryOperation,
		Action:   "usage_export." + action,
		Success:  action != "failed",
		Content:  fmt.Sprintf("usage export %s kind=%s format=%s%s", job.JobId, job.Kind, job.Format, detail),
	})
}

// RecordUsageExportDownloaded audits a download of a job's file.
func RecordUsageExportDownloaded(c *gin.Context, job *model.UsageExportJob) {
	recordUsageExportEvent(c, job, "downloaded", "")
}

// OpenUsageExportFile opens a succeeded job's file for download.
func OpenUsageExportFile(job *model.UsageExportJob) (io.ReadCloser, error) {
	return usageExportStore.Open(job.StorageKey)
}

// usageExportMaxAge is the stale threshold for running jobs, in seconds.
func usageExportMaxAge() int64 {
	return usageExportStaleSeconds
}
