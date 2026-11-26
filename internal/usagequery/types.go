// Package usagequery defines the inputs and read-only results of usage queries.
package usagequery

// Scope limits a usage report to the logs of one payer and, optionally, one user.
// Zero means the report does not restrict that field. The caller resolves the scope from
// the authenticated identity; request parameters never widen it on their own.
type Scope struct {
	OrganizationId int    // billing organization snapshot; 0 means every payer
	UserId         int    // user who made the request; 0 means every user
	ModelName      string // model the report is narrowed to; "" means every model
	PayerType      string // empty retains legacy behavior
}

// ExportGroupRow is the complete consumption of one user or billing organization.
// Name is the maximum logged username for users and empty for organizations.
type ExportGroupRow struct {
	Key              int64  `json:"key"`
	Name             string `json:"name"`
	ConsumeQuota     int64  `json:"consume_quota"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}
