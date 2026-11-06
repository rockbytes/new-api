package model

import "gorm.io/gorm"

// UsageScope limits a usage report to the logs of one payer and, optionally, one user.
// Zero means the report does not restrict that field. The caller resolves the scope from
// the authenticated identity; request parameters never widen it on their own.
type UsageScope struct {
	OrganizationId int    // billing organization snapshot; 0 means every payer
	UserId         int    // user who made the request; 0 means every user
	ModelName      string // model the report is narrowed to; "" means every model
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

// usageScoped applies the window and scope to a logs query. Only consume rows are reported.
// Logs are written at settlement, so pre-consumed amounts and failed requests are not counted.
func usageScoped(scope UsageScope, window UsageWindow) func(tx *gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("logs.created_at >= ? AND logs.created_at <= ?", window.Start, window.End).
			Where("logs.type = ?", LogTypeConsume)
		if scope.OrganizationId > 0 {
			tx = tx.Where("logs.billing_organization_id = ?", scope.OrganizationId)
		}
		if scope.UserId > 0 {
			tx = tx.Where("logs.user_id = ?", scope.UserId)
		}
		if scope.ModelName != "" {
			tx = tx.Where("logs.model_name = ?", scope.ModelName)
		}
		return tx
	}
}

// GetUsageModelNames returns every model with consume rows in the scope's window, ordered by
// name. The model filter is ignored, so the choices do not shrink when one model is selected.
func GetUsageModelNames(scope UsageScope, window UsageWindow) ([]string, error) {
	scope.ModelName = ""
	var names []string
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).
		Distinct("logs.model_name").Order("logs.model_name").Pluck("logs.model_name", &names).Error
	return names, err
}

// UsageSummary is the consumption and request totals of one scope.
type UsageSummary struct {
	ConsumeQuota     int64 `json:"consume_quota"`
	Requests         int64 `json:"requests"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// GetUsageSummary returns the totals of the scope within the window.
func GetUsageSummary(scope UsageScope, window UsageWindow) (UsageSummary, error) {
	var summary UsageSummary
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		"COALESCE(SUM(logs.quota), 0) AS consume_quota, " +
			"COUNT(*) AS requests, " +
			"COALESCE(SUM(logs.prompt_tokens), 0) AS prompt_tokens, " +
			"COALESCE(SUM(logs.completion_tokens), 0) AS completion_tokens",
	).Scan(&summary).Error
	return summary, err
}

// UsageTrendPoint is the consumption of one local day. DayStart is the Unix time of that
// day's local midnight.
type UsageTrendPoint struct {
	DayStart     int64 `json:"day_start"`
	ConsumeQuota int64 `json:"consume_quota"`
	Requests     int64 `json:"requests"`
}

// GetUsageTrend returns one point per local day that has usage, oldest first.
func GetUsageTrend(scope UsageScope, window UsageWindow) ([]UsageTrendPoint, error) {
	// Integer arithmetic only, so the bucket works the same on SQLite, MySQL and PostgreSQL.
	bucket := "(logs.created_at + ?) - ((logs.created_at + ?) % ?)"
	var points []UsageTrendPoint
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		bucket+" AS day_start, "+
			"COALESCE(SUM(logs.quota), 0) AS consume_quota, "+
			"COUNT(*) AS requests",
		window.TzOffsetSeconds, window.TzOffsetSeconds, int64(usageSecondsPerDay),
	).Group("day_start").Order("day_start").Scan(&points).Error
	if err != nil {
		return nil, err
	}
	for i := range points {
		points[i].DayStart -= window.TzOffsetSeconds
	}
	return points, nil
}

// UsageModelRow is the consumption of one model.
type UsageModelRow struct {
	ModelName        string `json:"model_name"`
	ConsumeQuota     int64  `json:"consume_quota"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// GetUsageModels returns the scope's consumption per model, largest first.
func GetUsageModels(scope UsageScope, window UsageWindow) ([]UsageModelRow, error) {
	var rows []UsageModelRow
	err := usageScoped(scope, window)(LOG_DB.Model(&Log{})).Select(
		"logs.model_name AS model_name, " +
			"COALESCE(SUM(logs.quota), 0) AS consume_quota, " +
			"COUNT(*) AS requests, " +
			"COALESCE(SUM(logs.prompt_tokens), 0) AS prompt_tokens, " +
			"COALESCE(SUM(logs.completion_tokens), 0) AS completion_tokens",
	).Group("logs.model_name").Order("consume_quota DESC").Limit(usageRankLimit).Scan(&rows).Error
	return rows, err
}

// UsageRankRow is the consumption of one user or one billing organization. Key is the user
// id or organization id, and 0 means personal billing (not in any organization).
type UsageRankRow struct {
	Key          int64  `json:"key" gorm:"column:rank_key"`
	Name         string `json:"name"`
	ConsumeQuota int64  `json:"consume_quota"`
	Requests     int64  `json:"requests"`
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
		keyColumn + " AS rank_key, " + nameColumn + " AS name, " +
			"COALESCE(SUM(logs.quota), 0) AS consume_quota, " +
			"COUNT(*) AS requests",
	).Group(keyColumn).Order("consume_quota DESC").Limit(usageRankLimit).Scan(&rows).Error
	return rows, err
}

// GetUsageRecords returns one page of the scope's consume rows, newest first, with the total
// number of rows. Callers must whitelist the fields they return.
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
