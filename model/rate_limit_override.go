package model

import (
	"gorm.io/gorm/clause"
)

// RateLimitOverrideSubjectOrganization is the subject type of an organization's
// total request limit.
const RateLimitOverrideSubjectOrganization = "organization"

// RateLimitOverrideSubjectUser is the subject type of a user's request limits.
const RateLimitOverrideSubjectUser = "user"

// RateLimitOverride is an administrator-set request limit for one subject. Only
// overridden subjects have a row, so the table grows with the number of
// overridden subjects rather than with all users or organizations. A count of 0
// means the subject does not override that count, so the global default applies.
type RateLimitOverride struct {
	Id           int    `json:"id"`
	SubjectType  string `json:"subject_type" gorm:"type:varchar(32);not null;uniqueIndex:uniq_rate_limit_override,priority:1"`
	SubjectId    int    `json:"subject_id" gorm:"not null;uniqueIndex:uniq_rate_limit_override,priority:2"`
	GroupName    string `json:"group_name" gorm:"type:varchar(64);not null;default:'';uniqueIndex:uniq_rate_limit_override,priority:3"`
	TotalCount   int    `json:"total_count" gorm:"not null;default:0"`
	SuccessCount int    `json:"success_count" gorm:"not null;default:0"`
	UpdatedBy    int    `json:"updated_by" gorm:"not null;default:0"`
	UpdatedTime  int64  `json:"updated_time" gorm:"bigint"`
}

// UpsertRateLimitOverride creates or replaces the override for one subject.
// It uses the unique index as the conflict target, which every supported
// database enforces.
func UpsertRateLimitOverride(override *RateLimitOverride) error {
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subject_type"}, {Name: "subject_id"}, {Name: "group_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"total_count", "success_count", "updated_by", "updated_time"}),
	}).Create(override).Error
}

// GetRateLimitOverride returns the override of one subject, or nil when it has none.
func GetRateLimitOverride(subjectType string, subjectId int) (*RateLimitOverride, error) {
	var override RateLimitOverride
	err := DB.Where("subject_type = ? AND subject_id = ? AND group_name = ?", subjectType, subjectId, "").
		Limit(1).Find(&override).Error
	if err != nil {
		return nil, err
	}
	if override.Id == 0 {
		return nil, nil
	}
	return &override, nil
}

// DeleteRateLimitOverride removes the override for one subject.
func DeleteRateLimitOverride(subjectType string, subjectId int) error {
	return DB.Where("subject_type = ? AND subject_id = ? AND group_name = ?", subjectType, subjectId, "").
		Delete(&RateLimitOverride{}).Error
}

// ListRateLimitOverrides returns every override of one subject type.
func ListRateLimitOverrides(subjectType string) ([]RateLimitOverride, error) {
	var overrides []RateLimitOverride
	err := DB.Where("subject_type = ?", subjectType).Order("subject_id asc").Find(&overrides).Error
	return overrides, err
}
