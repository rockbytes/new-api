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

package model

import (
	"errors"

	"gorm.io/gorm"
)

// Usage export job states. A job moves queued -> running -> succeeded | failed, and a
// succeeded job becomes expired when its file is deleted.
const (
	UsageExportStatusQueued    = "queued"
	UsageExportStatusRunning   = "running"
	UsageExportStatusSucceeded = "succeeded"
	UsageExportStatusFailed    = "failed"
	UsageExportStatusExpired   = "expired"
)

var ErrUsageExportNotFound = errors.New("usage export not found")

// UsageExportJob records one export: who asked, the scope intent, the filters, and where the
// result lives. StorageKey is internal and never sent to clients.
type UsageExportJob struct {
	Id              int    `json:"-"`
	JobId           string `json:"job_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId          int    `json:"user_id" gorm:"index"`
	View            string `json:"view" gorm:"type:varchar(32)"`
	OrganizationId  int    `json:"organization_id"`
	TargetUserId    int    `json:"target_user_id"`
	StartTimestamp  int64  `json:"start_timestamp" gorm:"type:bigint"`
	EndTimestamp    int64  `json:"end_timestamp" gorm:"type:bigint"`
	TzOffsetMinutes int    `json:"tz_offset_minutes"`
	Format          string `json:"format" gorm:"type:varchar(16)"`
	Kind            string `json:"kind" gorm:"type:varchar(32)"`
	WithBom         bool   `json:"with_bom"`
	Status          string `json:"status" gorm:"type:varchar(16);index"`
	RowCount        int64  `json:"row_count" gorm:"type:bigint"`
	FileName        string `json:"file_name" gorm:"type:varchar(255)"`
	StorageKey      string `json:"-" gorm:"type:varchar(255)"`
	ErrorCode       string `json:"error_code" gorm:"type:varchar(64)"`
	CreatedTime     int64  `json:"created_time" gorm:"type:bigint"`
	StartedTime     int64  `json:"started_time" gorm:"type:bigint"`
	FinishedTime    int64  `json:"finished_time" gorm:"type:bigint"`
	ExpiresTime     int64  `json:"expires_time" gorm:"type:bigint;index"`
}

func CreateUsageExportJob(job *UsageExportJob) error {
	return DB.Create(job).Error
}

func GetUsageExportJob(jobId string) (*UsageExportJob, error) {
	var job UsageExportJob
	err := DB.Where("job_id = ?", jobId).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUsageExportNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ListUsageExportJobs returns the newest jobs of one user.
func ListUsageExportJobs(userId int, limit int) ([]UsageExportJob, error) {
	var jobs []UsageExportJob
	err := DB.Where("user_id = ?", userId).Order("id DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// CountActiveUsageExportJobs counts a user's queued and running jobs.
func CountActiveUsageExportJobs(userId int) (int64, error) {
	var total int64
	err := DB.Model(&UsageExportJob{}).
		Where("user_id = ? AND status IN ?", userId, []string{UsageExportStatusQueued, UsageExportStatusRunning}).
		Count(&total).Error
	return total, err
}

// ClaimUsageExportJob moves a queued job to running. Only one caller wins, because the
// update matches the queued state.
func ClaimUsageExportJob(jobId string, now int64) (bool, error) {
	result := DB.Model(&UsageExportJob{}).
		Where("job_id = ? AND status = ?", jobId, UsageExportStatusQueued).
		Updates(map[string]any{"status": UsageExportStatusRunning, "started_time": now})
	return result.RowsAffected == 1, result.Error
}

// FinishUsageExportJob records the terminal state of a running job.
func FinishUsageExportJob(jobId string, status string, rowCount int64, fileName string, storageKey string, errorCode string, now int64, expires int64) error {
	return DB.Model(&UsageExportJob{}).
		Where("job_id = ? AND status = ?", jobId, UsageExportStatusRunning).
		Updates(map[string]any{
			"status":        status,
			"row_count":     rowCount,
			"file_name":     fileName,
			"storage_key":   storageKey,
			"error_code":    errorCode,
			"finished_time": now,
			"expires_time":  expires,
		}).Error
}

// FailStaleUsageExportJobs fails running jobs that started before the cutoff, for example
// when the process that owned them stopped.
func FailStaleUsageExportJobs(startedBefore int64, now int64) (int64, error) {
	result := DB.Model(&UsageExportJob{}).
		Where("status = ? AND started_time > 0 AND started_time < ?", UsageExportStatusRunning, startedBefore).
		Updates(map[string]any{"status": UsageExportStatusFailed, "error_code": "USAGE_EXPORT_STALE", "finished_time": now})
	return result.RowsAffected, result.Error
}

// ListExpiredUsageExportJobs returns succeeded jobs whose file should be deleted.
func ListExpiredUsageExportJobs(now int64, limit int) ([]UsageExportJob, error) {
	var jobs []UsageExportJob
	err := DB.Where("status = ? AND expires_time > 0 AND expires_time <= ?", UsageExportStatusSucceeded, now).
		Order("id ASC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// MarkUsageExportJobExpired records that the job's file has been deleted.
func MarkUsageExportJobExpired(jobId string) error {
	return DB.Model(&UsageExportJob{}).
		Where("job_id = ? AND status = ?", jobId, UsageExportStatusSucceeded).
		Update("status", UsageExportStatusExpired).Error
}

// CountUsageRecords returns how many consume and refund rows a scope has in the window.
func CountUsageRecords(scope UsageScope, window UsageWindow) (int64, error) {
	var total int64
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Count(&total).Error
	return total, err
}

// ForEachUsageRecord streams the scope's consume and refund rows in batches, oldest id first.
// Keyset paging keeps memory bounded however many rows the scope has.
func ForEachUsageRecord(scope UsageScope, window UsageWindow, batchSize int, visit func(Log) error) error {
	var lastId int
	for {
		var batch []Log
		err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).
			Where("logs.id > ?", lastId).
			Order("logs.id ASC").Limit(batchSize).Find(&batch).Error
		if err != nil {
			return err
		}
		for _, row := range batch {
			if err := visit(row); err != nil {
				return err
			}
			lastId = row.Id
		}
		if len(batch) < batchSize {
			return nil
		}
	}
}
