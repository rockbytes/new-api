package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PersonalKeySettlement is a primary-DB receipt, not a usage log. Its request ID
// comes from the gateway, never the client. It is retained for replay protection.
type PersonalKeySettlement struct {
	RequestId      string `gorm:"primaryKey;size:128"`
	UserId         int
	TokenId        int
	Quota          int
	SubscriptionId int
	FundingDelta   int
	Committed      int16 `gorm:"not null"` // 0 pending; 1 recorded
}

// SettlePersonalKey atomically commits a confirmed personal charge and its gross
// usage. An exact replay is a no-op; a conflicting replay does not move funds.
// fundingDelta is the charge difference (negative means credit), not the total.
func SettlePersonalKey(requestId string, userId, tokenId, quota, fundingDelta, subscriptionId int, period *int64) (bool, error) {
	if requestId == "" || len(requestId) > 128 || userId <= 0 || tokenId <= 0 || quota <= 0 || subscriptionId < 0 || fundingDelta > quota || fundingDelta < -common.MaxQuota {
		return false, errors.New("invalid personal Key settlement")
	}
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		receipt := PersonalKeySettlement{RequestId: requestId, UserId: userId, TokenId: tokenId, Quota: quota, SubscriptionId: subscriptionId, FundingDelta: fundingDelta}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt).Error; err != nil {
			return err
		}
		var prior PersonalKeySettlement
		if err := lockForUpdate(tx).First(&prior, "request_id = ?", requestId).Error; err != nil {
			return err
		}
		receipt.Committed = prior.Committed
		if prior != receipt {
			return errors.New("conflicting personal Key settlement replay")
		}
		if prior.Committed == 1 {
			return nil
		}
		if err := settlePersonalKeyFundsTx(tx, userId, quota, fundingDelta, subscriptionId, period, 0); err != nil {
			return err
		}
		if err := tx.Model(&prior).UpdateColumn("committed", 1).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied && subscriptionId == 0 {
		syncPersonalSettlementCache(userId, fundingDelta)
	}
	return applied && err == nil, err
}

func settlePersonalKeyFundsTx(tx *gorm.DB, userId, grossQuota, delta, subscriptionId int, period *int64, reservedAt int64) error {
	if subscriptionId > 0 {
		if delta > 0 {
			period, reservedAt = nil, 0 // keep existing positive settlement policy
		}
		if err := adjustSubscriptionUsageTx(tx, subscriptionId, int64(delta), period, reservedAt); err != nil {
			return err
		}
	}
	user := tx.Unscoped().Model(&User{}).Where("id = ?", userId)
	if subscriptionId == 0 && delta < 0 {
		if delta < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		user = user.Where("quota <= ?", common.MaxWalletQuota+delta)
	}
	updates := make(map[string]any, 2)
	if subscriptionId == 0 && delta != 0 {
		updates["quota"] = gorm.Expr("quota - ?", delta)
	}
	if grossQuota > 0 {
		updates["personal_key_used_quota"] = gorm.Expr(
			"CASE WHEN personal_key_used_quota > ? THEN ? ELSE personal_key_used_quota + ? END",
			math.MaxInt64-int64(grossQuota), int64(math.MaxInt64), grossQuota)
	}
	if len(updates) == 0 {
		return nil
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) && grossQuota > 0 && (subscriptionId > 0 || delta == 0) {
		// A saturated counter is a successful no-op, even with changed-row mode.
		var row User
		if err := lockForUpdate(user.Session(&gorm.Session{})).Select("id").Take(&row).Error; err != nil {
			return err
		}
		return user.Updates(updates).Error
	}
	result := user.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("personal Key settlement account missing or quota limit exceeded")
	}
	return nil
}

func syncPersonalSettlementCache(userId, delta int) {
	if delta != 0 {
		if err := cacheDecrUserQuota(userId, int64(delta)); err != nil {
			common.SysError(common.LogText("failed to sync personal Key settlement cache: %s", err.Error()))
		}
	}
}

// SettlePersonalKeyTask uses the persisted task quota as the replay guard. The
// target quota, funding, and positive usage delta are written together; the
// private token snapshot remains valid after the Key has been removed or rebound.
func SettlePersonalKeyTask(task *Task, target int) (bool, error) {
	if task.ID <= 0 || task.Quota < 0 || target < 0 || task.PrivateData.TokenId <= 0 || task.BillingOrganizationId != 0 {
		return false, errors.New("invalid personal Key task settlement")
	}
	delta := target - task.Quota
	if delta == 0 {
		return false, nil
	}
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Task{}).Where("id = ? AND quota = ? AND status <> ?", task.ID, task.Quota, TaskStatusFailure).UpdateColumn("quota", target)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		var current Task
		if err := tx.Omit("data").First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.UserId != task.UserId || current.BillingOrganizationId != 0 || current.PrivateData.TokenId != task.PrivateData.TokenId ||
			current.PrivateData.SubscriptionId != task.PrivateData.SubscriptionId {
			return errors.New("task settlement payer changed")
		}
		subscriptionId := 0
		switch current.PrivateData.BillingSource {
		case "subscription":
			subscriptionId = current.PrivateData.SubscriptionId
			if subscriptionId <= 0 {
				return errors.New("task settlement subscription is missing")
			}
		case "", "wallet":
			if current.PrivateData.SubscriptionId != 0 {
				return errors.New("unexpected subscription for wallet-paid task")
			}
		default:
			return errors.New("unknown personal Key task funding source")
		}
		if err := settlePersonalKeyFundsTx(tx, current.UserId, max(delta, 0), delta, subscriptionId, current.PrivateData.SubscriptionPeriodStart, current.CreatedAt); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied && task.PrivateData.SubscriptionId == 0 {
		syncPersonalSettlementCache(task.UserId, delta)
	}
	return applied && err == nil, err
}
