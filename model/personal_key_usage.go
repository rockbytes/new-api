package model

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const personalKeyUsageBackfillKey = "PersonalKeyUsageBackfillV1"
const personalKeyUsageBackfillComplete = "complete"

// InitializePersonalKeyUsage imports the attributable history once, before this
// binary starts serving requests. Stop old writers and log cleanup before the
// first upgraded primary starts; logs already removed cannot be reconstructed.
func InitializePersonalKeyUsage() error {
	var marker Option
	err := DB.Where(&Option{Key: personalKeyUsageBackfillKey}).Take(&marker).Error
	if err == nil && marker.Value == personalKeyUsageBackfillComplete {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !common.IsMasterNode {
		return errors.New("personal Key usage has not been initialized; upgrade the primary node first")
	}
	if err := checkPersonalKeyMaintenance(); err != nil {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		// Inserting the marker serializes concurrent primary startups. The marker
		// and the totals commit together; a failed import leaves neither behind.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: personalKeyUsageBackfillKey}).Error; err != nil {
			return err
		}
		var marker Option
		if err := lockForUpdate(tx).Where(&Option{Key: personalKeyUsageBackfillKey}).Take(&marker).Error; err != nil {
			return err
		}
		if marker.Value == personalKeyUsageBackfillComplete {
			return nil
		}

		logs := LOG_DB
		if logs == nil {
			return errors.New("usage log database is unavailable for the initial import")
		}
		if logs == DB {
			logs = tx
		}
		var totals []struct {
			UserId       int
			ConsumeQuota int64
		}
		// Historical task supplements had no payer snapshot. Do not misattribute
		// those rows to personal Keys just because the organization ID is zero.
		if err := logs.Model(&Log{}).
			Select("user_id, SUM(quota) AS consume_quota").
			Where("user_id > 0 AND type = ? AND token_id > 0 AND billing_organization_id = 0 AND quota > 0", LogTypeConsume).
			Where("COALESCE(other, '') NOT LIKE ?", `%"violation_fee":true%`).
			Where("(COALESCE(other, '') NOT LIKE ? OR other LIKE ?)", `%"pre_consumed_quota":%`, `%"`+taskBillingPayerRecordedKey+`":true%`).
			Group("user_id").Scan(&totals).Error; err != nil {
			return fmt.Errorf("read historical personal Key usage: %w", err)
		}
		for _, row := range totals {
			if err := tx.Unscoped().Model(&User{}).Where("id = ?", row.UserId).
				Update("personal_key_used_quota", row.ConsumeQuota).Error; err != nil {
				return fmt.Errorf("initialize personal Key usage for user %d: %w", row.UserId, err)
			}
		}
		return tx.Model(&marker).Update("value", personalKeyUsageBackfillComplete).Error
	})
}

// checkPersonalKeyMaintenance prevents the managed Linux gateway from importing
// while the stock slave-first upgrade (or old writers) is still active.
func checkPersonalKeyMaintenance() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	const releases = "/opt/new-api/releases/"
	path := filepath.ToSlash(resolved)
	if !strings.HasPrefix(path, releases) {
		return nil
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(path, releases), "/")
	const tokenPath = "/run/new-api-billing-maintenance"
	info, err := os.Lstat(tokenPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&022 != 0 || !ownedByRoot(info) {
		return errors.New("first personal Key import requires the gateway billing maintenance upgrade")
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil || strings.TrimSpace(string(data)) != version {
		return errors.New("billing maintenance authorization does not match this release")
	}
	return nil
}

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
