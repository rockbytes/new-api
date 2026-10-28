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
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const usageExportCleanupTaskType = "usage_export_cleanup"

// usageExportCleanupBatch bounds how many expired files one run deletes.
const usageExportCleanupBatch = 200

// usageExportCleanupHandler fails stale running jobs and deletes expired export files. A file
// that cannot be deleted stays on its job, so the next run retries it.
type usageExportCleanupHandler struct{}

type usageExportCleanupPayload struct{}

func (usageExportCleanupHandler) Type() string { return usageExportCleanupTaskType }

func (usageExportCleanupHandler) Enabled() bool { return true }

func (usageExportCleanupHandler) Interval() time.Duration { return time.Hour }

func (usageExportCleanupHandler) NewPayload() any { return usageExportCleanupPayload{} }

func (usageExportCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := common.GetTimestamp()
	if stale, err := model.FailStaleUsageExportJobs(now-usageExportMaxAge(), now); err != nil {
		common.SysError("usage export cleanup: fail stale jobs: " + err.Error())
	} else if stale > 0 {
		common.SysLog("usage export cleanup: failed stale jobs: " + common.GetJsonString(stale))
	}

	expired, err := model.ListExpiredUsageExportJobs(now, usageExportCleanupBatch)
	if err != nil {
		common.SysError("usage export cleanup: list expired jobs: " + err.Error())
	}
	for i := range expired {
		job := expired[i]
		if err := usageExportStore.Delete(job.StorageKey); err != nil {
			common.SysError("usage export cleanup: delete " + job.JobId + ": " + err.Error())
			continue
		}
		if err := model.MarkUsageExportJobExpired(job.JobId); err != nil {
			common.SysError("usage export cleanup: mark " + job.JobId + " expired: " + err.Error())
			continue
		}
		common.SysLog("usage export cleanup: expired " + job.JobId)
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, nil, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func init() {
	RegisterSystemTaskHandler(usageExportCleanupHandler{})
}
