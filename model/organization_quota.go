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
	"fmt"
	"math"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Organization quota ledger event types. Every money movement is recorded once under
// an idempotency key made of the event type, the organization, and a caller reference
// (request or task ID, or a generated operator ID), so a retried call cannot apply twice.
const (
	OrganizationLedgerGrant      = "grant"      // platform grant that raises the organization total (D8)
	OrganizationLedgerPreConsume = "preconsume" // usage reserved before a request runs
	OrganizationLedgerSettle     = "settle"     // additional usage charged after a request's final quota is known
	OrganizationLedgerRefund     = "refund"     // usage returned to the organization and the member
	OrganizationLedgerShortfall  = "shortfall"  // settlement excess that could not be charged (no overdraft)
)

const organizationLedgerKeyMaxLength = 191

var (
	ErrOrganizationQuotaInvalidAmount      = errors.New("organization quota amount must be a positive integer")
	ErrOrganizationQuotaInsufficient       = errors.New("insufficient organization quota")
	ErrOrganizationMemberQuotaInsufficient = errors.New("insufficient member quota")
	ErrOrganizationMemberQuotaBelowUsage   = errors.New("member allocation cannot be below the usage already recorded")
	ErrOrganizationMemberRoleNotAllowed    = errors.New("organization admins can only set the allocation of ordinary members")
	ErrOrganizationQuotaOverflow           = errors.New("organization quota total would overflow")
	ErrOrganizationLedgerRefInvalid        = errors.New("organization ledger reference is invalid")
)

// OrganizationQuotaAccount is the organization's quota. The organization's remaining
// quota is GrantedQuota minus ConsumedQuota. Allocations are limits and are not
// subtracted from it, so they may sum to more than the remaining quota.
type OrganizationQuotaAccount struct {
	OrganizationId int   `json:"organization_id" gorm:"primaryKey"`
	GrantedQuota   int64 `json:"granted_quota" gorm:"bigint;not null;default:0"`
	ConsumedQuota  int64 `json:"consumed_quota" gorm:"bigint;not null;default:0"`
	UpdatedTime    int64 `json:"updated_time" gorm:"bigint"`
}

// OrganizationMemberQuota is a member's allocation: the number an organization admin
// or Root sets, and the usage recorded against it. Remaining is AllocatedQuota minus
// UsedQuota. The row is kept when the member is disabled or deleted, so usage stays
// attributed to the organization.
type OrganizationMemberQuota struct {
	Id             int   `json:"id"`
	OrganizationId int   `json:"organization_id" gorm:"not null;uniqueIndex:uniq_org_member_quota,priority:1"`
	UserId         int   `json:"user_id" gorm:"not null;uniqueIndex:uniq_org_member_quota,priority:2"`
	AllocatedQuota int64 `json:"allocated_quota" gorm:"bigint;not null;default:0"`
	UsedQuota      int64 `json:"used_quota" gorm:"bigint;not null;default:0"`
	UpdatedTime    int64 `json:"updated_time" gorm:"bigint"`
}

// OrganizationQuotaLedgerEntry is an append-only record of one money movement.
// Amount is always positive; the event type gives the direction.
type OrganizationQuotaLedgerEntry struct {
	Id             int    `json:"id"`
	OrganizationId int    `json:"organization_id" gorm:"not null;index:idx_org_ledger_org_time,priority:1"`
	EventType      string `json:"event_type" gorm:"type:varchar(32);not null"`
	Amount         int64  `json:"amount" gorm:"bigint;not null"`
	TargetUserId   int    `json:"target_user_id"`
	ActorUserId    int    `json:"actor_user_id"`
	RequestId      string `json:"request_id" gorm:"type:varchar(64);index"`
	Reason         string `json:"reason" gorm:"type:varchar(255)"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(191);not null;uniqueIndex"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint;index:idx_org_ledger_org_time,priority:2"`
}

// OrganizationQuotaReconciliation checks the organization's quota. Usage on the
// organization must equal the sum of member usage, and usage must not exceed the total.
type OrganizationQuotaReconciliation struct {
	GrantedQuota   int64 `json:"granted_quota"`
	ConsumedQuota  int64 `json:"consumed_quota"`
	RemainingQuota int64 `json:"remaining_quota"`
	MemberUsed     int64 `json:"member_used"`
	Allocated      int64 `json:"allocated"`
	Balanced       bool  `json:"balanced"`
}

// organizationLedgerKey builds the idempotency key for one event. The reference is
// required so a retried call cannot apply the same event twice.
func organizationLedgerKey(eventType string, organizationId int, ref string) (string, error) {
	if ref == "" {
		return "", ErrOrganizationLedgerRefInvalid
	}
	key := fmt.Sprintf("%s:%d:%s", eventType, organizationId, ref)
	if len(key) > organizationLedgerKeyMaxLength {
		return "", ErrOrganizationLedgerRefInvalid
	}
	return key, nil
}

func addOrganizationQuota(a int64, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, ErrOrganizationQuotaOverflow
	}
	return a + b, nil
}

// lockOrganizationQuotaAccount returns the organization's account, creating it on
// first use, and locks it for the rest of the transaction.
func lockOrganizationQuotaAccount(tx *gorm.DB, organizationId int, now int64) (*OrganizationQuotaAccount, error) {
	account := &OrganizationQuotaAccount{OrganizationId: organizationId, UpdatedTime: now}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(account).Error; err != nil {
		return nil, err
	}
	locked := &OrganizationQuotaAccount{}
	if err := lockForUpdate(tx).Where("organization_id = ?", organizationId).First(locked).Error; err != nil {
		return nil, err
	}
	return locked, nil
}

// lockOrganizationMemberQuota returns a member's allocation row, creating it with a zero
// allocation on first use, and locks it for the rest of the transaction.
func lockOrganizationMemberQuota(tx *gorm.DB, organizationId int, userId int, now int64) (*OrganizationMemberQuota, error) {
	row := &OrganizationMemberQuota{OrganizationId: organizationId, UserId: userId, UpdatedTime: now}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return nil, err
	}
	locked := &OrganizationMemberQuota{}
	if err := lockForUpdate(tx).Where("organization_id = ? AND user_id = ?", organizationId, userId).First(locked).Error; err != nil {
		return nil, err
	}
	return locked, nil
}

// recordOrganizationLedgerEntry appends an event. It reports false, with no error,
// when the same event was already applied, so the caller skips the balance update.
func recordOrganizationLedgerEntry(tx *gorm.DB, entry *OrganizationQuotaLedgerEntry) (bool, error) {
	var existing int64
	if err := tx.Model(&OrganizationQuotaLedgerEntry{}).Where("idempotency_key = ?", entry.IdempotencyKey).Count(&existing).Error; err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	if err := tx.Create(entry).Error; err != nil {
		return false, err
	}
	return true, nil
}

// GrantOrganizationQuota raises the organization's total by a platform grant (D8). It
// reports false when the same grant reference was already applied.
func GrantOrganizationQuota(organizationId int, amount int64, actorUserId int, reason string, ref string, now int64) (bool, error) {
	if amount <= 0 {
		return false, ErrOrganizationQuotaInvalidAmount
	}
	key, err := organizationLedgerKey(OrganizationLedgerGrant, organizationId, ref)
	if err != nil {
		return false, err
	}
	applied := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		account, err := lockOrganizationQuotaAccount(tx, organizationId, now)
		if err != nil {
			return err
		}
		granted, err := addOrganizationQuota(account.GrantedQuota, amount)
		if err != nil {
			return err
		}
		recorded, err := recordOrganizationLedgerEntry(tx, &OrganizationQuotaLedgerEntry{
			OrganizationId: organizationId, EventType: OrganizationLedgerGrant, Amount: amount,
			ActorUserId: actorUserId, Reason: reason, IdempotencyKey: key, CreatedTime: now,
		})
		if err != nil || !recorded {
			return err
		}
		applied = true
		return tx.Model(&OrganizationQuotaAccount{}).Where("organization_id = ?", organizationId).Updates(map[string]any{
			"granted_quota": granted,
			"updated_time":  now,
		}).Error
	})
	return applied, err
}

// SetOrganizationMemberAllocation sets a member's allocation. It is a limit, not a
// transfer, so it does not move quota and it may exceed the organization's remaining
// quota (B). It cannot go below the usage the member has already recorded. When
// membersOnly is set, the target must be an ordinary member, which is how organization
// admins are limited (they cannot set their own or other admins' allocations).
func SetOrganizationMemberAllocation(organizationId int, userId int, allocated int64, membersOnly bool, now int64) error {
	if allocated < 0 {
		return ErrOrganizationQuotaInvalidAmount
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		membership, err := getOrganizationMemberTx(tx, organizationId, userId)
		if err != nil {
			return err
		}
		if membersOnly && membership.Role != OrganizationRoleMember {
			return ErrOrganizationMemberRoleNotAllowed
		}
		row, err := lockOrganizationMemberQuota(tx, organizationId, userId, now)
		if err != nil {
			return err
		}
		if allocated < row.UsedQuota {
			return ErrOrganizationMemberQuotaBelowUsage
		}
		return tx.Model(&OrganizationMemberQuota{}).Where("organization_id = ? AND user_id = ?", organizationId, userId).Updates(map[string]any{
			"allocated_quota": allocated,
			"updated_time":    now,
		}).Error
	})
}

// GetOrganizationQuotaReconciliation checks the quota equations for an organization.
func GetOrganizationQuotaReconciliation(organizationId int) (*OrganizationQuotaReconciliation, error) {
	account := &OrganizationQuotaAccount{}
	if err := DB.Where("organization_id = ?", organizationId).First(account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &OrganizationQuotaReconciliation{Balanced: true}, nil
		}
		return nil, err
	}
	var memberUsed, allocated int64
	if err := DB.Model(&OrganizationMemberQuota{}).Select("COALESCE(SUM(used_quota), 0)").Where("organization_id = ?", organizationId).Scan(&memberUsed).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&OrganizationMemberQuota{}).Select("COALESCE(SUM(allocated_quota), 0)").Where("organization_id = ?", organizationId).Scan(&allocated).Error; err != nil {
		return nil, err
	}
	return &OrganizationQuotaReconciliation{
		GrantedQuota:   account.GrantedQuota,
		ConsumedQuota:  account.ConsumedQuota,
		RemainingQuota: account.GrantedQuota - account.ConsumedQuota,
		MemberUsed:     memberUsed,
		Allocated:      allocated,
		Balanced:       account.ConsumedQuota == memberUsed && account.GrantedQuota >= account.ConsumedQuota,
	}, nil
}
