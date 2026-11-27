package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserModelRateLimit replaces the default limits for one user. Zero disables
// the corresponding limit; inheriting the defaults is represented by no row.
type UserModelRateLimit struct {
	UserID  int `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Total   int `json:"total" gorm:"not null"`
	Success int `json:"success" gorm:"not null"`
}

// GetUserModelRateLimit returns nil when the user inherits the default limits.
func GetUserModelRateLimit(ctx context.Context, userID int) (*UserModelRateLimit, error) {
	var limit UserModelRateLimit
	err := DB.WithContext(ctx).Where("user_id = ?", userID).Take(&limit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &limit, nil
}

// SetModelRateLimit applies an override (or clears it with nil) only while the
// user's role still matches the role the caller authorized against.
func (user *User) SetModelRateLimit(ctx context.Context, limit *UserModelRateLimit) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target User
		if err := lockForUpdate(tx).Select("id").Where("id = ? AND role = ?", user.Id, user.Role).Take(&target).Error; err != nil {
			return err
		}
		if limit == nil {
			return tx.Delete(&UserModelRateLimit{}, "user_id = ?", user.Id).Error
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"total", "success"}),
		}).Create(&UserModelRateLimit{UserID: user.Id, Total: limit.Total, Success: limit.Success}).Error
	})
}
