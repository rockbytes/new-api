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

import "gorm.io/gorm"

// UsageScope limits a usage report to the logs of one payer and, optionally, one user.
// Zero means the report does not restrict that field. The caller resolves the scope from
// the authenticated identity; request parameters never widen it on their own.
type UsageScope struct {
	OrganizationId int // billing organization snapshot; 0 means every payer
	UserId         int // user who made the request; 0 means every user
}

// UsageWindow is the time range of a report. TzOffsetSeconds shifts day boundaries to the
// caller's local time, so trend buckets start at local midnight.
type UsageWindow struct {
	Start           int64
	End             int64
	TzOffsetSeconds int64
}

const (
	usageSecondsPerDay = 86400
	usageRankLimit     = 50
)

// usageNetBasis is the reported consumption: settled consume rows minus refund rows. Logs
// are written at settlement, so pre-consumed amounts and failed requests are not counted.
const usageNetBasis = "COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.quota WHEN logs.type = ? THEN -logs.quota ELSE 0 END), 0)"

// usageScoped applies the window and scope to a logs query. Only consume and refund rows
// are reported; every other log type is outside the usage basis.
func usageScoped(scope UsageScope, window UsageWindow) func(tx *gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("logs.created_at >= ? AND logs.created_at <= ?", window.Start, window.End).
			Where("logs.type IN ?", []int{LogTypeConsume, LogTypeRefund})
		if scope.OrganizationId > 0 {
			tx = tx.Where("logs.billing_organization_id = ?", scope.OrganizationId)
		}
		if scope.UserId > 0 {
			tx = tx.Where("logs.user_id = ?", scope.UserId)
		}
		return tx
	}
}

// UsageSummary is the net consumption and request totals of one scope.
type UsageSummary struct {
	NetQuota         int64 `json:"net_quota"`
	ConsumeQuota     int64 `json:"consume_quota"`
	RefundQuota      int64 `json:"refund_quota"`
	Requests         int64 `json:"requests"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// GetUsageSummary returns the totals of the scope within the window.
func GetUsageSummary(scope UsageScope, window UsageWindow) (UsageSummary, error) {
	var summary UsageSummary
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.quota ELSE 0 END), 0) AS consume_quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.quota ELSE 0 END), 0) AS refund_quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END), 0) AS requests, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.prompt_tokens ELSE 0 END), 0) AS prompt_tokens, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.completion_tokens ELSE 0 END), 0) AS completion_tokens",
		LogTypeConsume, LogTypeRefund, LogTypeConsume, LogTypeConsume, LogTypeConsume,
	).Scan(&summary).Error
	summary.NetQuota = summary.ConsumeQuota - summary.RefundQuota
	return summary, err
}

// UsageTrendPoint is the net consumption of one local day. DayStart is the Unix time of
// that day's local midnight.
type UsageTrendPoint struct {
	DayStart int64 `json:"day_start"`
	NetQuota int64 `json:"net_quota"`
	Requests int64 `json:"requests"`
}

// GetUsageTrend returns one point per local day that has usage, oldest first.
func GetUsageTrend(scope UsageScope, window UsageWindow) ([]UsageTrendPoint, error) {
	// Integer arithmetic only, so the bucket works the same on SQLite, MySQL and PostgreSQL.
	bucket := "(logs.created_at + ?) - ((logs.created_at + ?) % ?)"
	var points []UsageTrendPoint
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		bucket+" AS day_start, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.quota WHEN logs.type = ? THEN -logs.quota ELSE 0 END), 0) AS net_quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END), 0) AS requests",
		window.TzOffsetSeconds, window.TzOffsetSeconds, int64(usageSecondsPerDay),
		LogTypeConsume, LogTypeRefund, LogTypeConsume,
	).Group("day_start").Order("day_start").Scan(&points).Error
	if err != nil {
		return nil, err
	}
	for i := range points {
		points[i].DayStart -= window.TzOffsetSeconds
	}
	return points, nil
}

// UsageModelRow is the net consumption of one model.
type UsageModelRow struct {
	ModelName        string `json:"model_name"`
	NetQuota         int64  `json:"net_quota"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// GetUsageModels returns the scope's consumption per model, largest first.
func GetUsageModels(scope UsageScope, window UsageWindow) ([]UsageModelRow, error) {
	var rows []UsageModelRow
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		"logs.model_name AS model_name, "+
			usageNetBasis+" AS net_quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END), 0) AS requests, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.prompt_tokens ELSE 0 END), 0) AS prompt_tokens, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN logs.completion_tokens ELSE 0 END), 0) AS completion_tokens",
		LogTypeConsume, LogTypeRefund, LogTypeConsume, LogTypeConsume, LogTypeConsume,
	).Group("logs.model_name").Order("net_quota DESC").Limit(usageRankLimit).Scan(&rows).Error
	return rows, err
}

// UsageRankRow is the net consumption of one user or one billing organization. Key is the
// user id or organization id, and 0 means personal billing (not in any organization).
type UsageRankRow struct {
	Key      int64  `json:"key" gorm:"column:rank_key"`
	Name     string `json:"name"`
	NetQuota int64  `json:"net_quota"`
	Requests int64  `json:"requests"`
}

// UsageRankBy selects what a ranking groups by.
type UsageRankBy string

const (
	UsageRankByUser         UsageRankBy = "user"
	UsageRankByOrganization UsageRankBy = "organization"
)

// GetUsageRanking returns the largest consumers within the scope, largest first.
func GetUsageRanking(scope UsageScope, window UsageWindow, by UsageRankBy) ([]UsageRankRow, error) {
	keyColumn := "logs.user_id"
	nameColumn := "MAX(logs.username)"
	if by == UsageRankByOrganization {
		keyColumn = "logs.billing_organization_id"
		nameColumn = "''"
	}
	var rows []UsageRankRow
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		keyColumn+" AS rank_key, "+nameColumn+" AS name, "+
			usageNetBasis+" AS net_quota, "+
			"COALESCE(SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END), 0) AS requests",
		LogTypeConsume, LogTypeRefund, LogTypeConsume,
	).Group(keyColumn).Order("net_quota DESC").Limit(usageRankLimit).Scan(&rows).Error
	return rows, err
}

// GetUsageRecords returns one page of the scope's consume and refund rows, newest first,
// with the total number of rows. Callers must whitelist the fields they return.
func GetUsageRecords(scope UsageScope, window UsageWindow, page int, pageSize int) ([]Log, int64, error) {
	base := usageScoped(scope, window)(LOG_DB.Model(&Log{}))
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []Log
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).
		Order("logs.created_at DESC, logs.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&logs).Error
	return logs, total, err
}
