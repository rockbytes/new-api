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

	"gorm.io/gorm"
)

var (
	ErrOrganizationRequestUnknown = errors.New("organization request has no pre-consumed quota")
	ErrOrganizationRefundExceeded = errors.New("refund exceeds the settled quota of the request")
)

// organizationRemaining returns what the member and the organization each still allow.
func organizationRemaining(account *OrganizationQuotaAccount, member *OrganizationMemberQuota) (memberRemaining int64, orgRemaining int64) {
	return member.AllocatedQuota - member.UsedQuota, account.GrantedQuota - account.ConsumedQuota
}

// applyOrganizationUsage adds delta to the organization's usage and the member's usage.
// The caller holds both locks and has checked the limits.
func applyOrganizationUsage(tx *gorm.DB, organizationId int, userId int, delta int64, now int64) error {
	if err := tx.Model(&OrganizationQuotaAccount{}).Where("organization_id = ?", organizationId).Updates(map[string]any{
		"consumed_quota": gorm.Expr("consumed_quota + ?", delta),
		"updated_time":   now,
	}).Error; err != nil {
		return err
	}
	return tx.Model(&OrganizationMemberQuota{}).Where("organization_id = ? AND user_id = ?", organizationId, userId).Updates(map[string]any{
		"used_quota":   gorm.Expr("used_quota + ?", delta),
		"updated_time": now,
	}).Error
}

// ConsumeOrganizationQuota reserves usage for a request before it runs. Both the
// member's remaining allocation and the organization's remaining quota must cover the
// amount, so neither can go negative. A disabled member cannot consume.
func ConsumeOrganizationQuota(organizationId int, userId int, requestId string, seq int, amount int64, now int64) error {
	if amount <= 0 {
		return ErrOrganizationQuotaInvalidAmount
	}
	key, err := organizationLedgerKey(OrganizationLedgerPreConsume, organizationId, fmt.Sprintf("%s:%d", requestId, seq))
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		organization, err := lockOrganization(tx, organizationId)
		if err != nil {
			return err
		}
		if organization.Status != OrganizationStatusActive {
			return ErrOrganizationNotActive
		}
		membership, err := getOrganizationMemberTx(tx, organizationId, userId)
		if err != nil {
			return err
		}
		if membership.Status != OrganizationMembershipActive {
			return ErrOrganizationMembershipMissing
		}
		account, err := lockOrganizationQuotaAccount(tx, organizationId, now)
		if err != nil {
			return err
		}
		member, err := lockOrganizationMemberQuota(tx, organizationId, userId, now)
		if err != nil {
			return err
		}
		memberRemaining, orgRemaining := organizationRemaining(account, member)
		if memberRemaining < amount {
			return ErrOrganizationMemberQuotaInsufficient
		}
		if orgRemaining < amount {
			return ErrOrganizationQuotaInsufficient
		}
		recorded, err := recordOrganizationLedgerEntry(tx, &OrganizationQuotaLedgerEntry{
			OrganizationId: organizationId, EventType: OrganizationLedgerPreConsume, Amount: amount, TargetUserId: userId,
			RequestId: requestId, IdempotencyKey: key, CreatedTime: now,
		})
		if err != nil || !recorded {
			return err
		}
		return applyOrganizationUsage(tx, organizationId, userId, amount, now)
	})
}

// SettleOrganizationQuota applies the difference between a request's final quota and
// what was pre-consumed. A positive delta is charged only up to the smaller of the
// member's and the organization's remaining quota; the excess is recorded as shortfall
// and is not charged. A negative delta is refunded. It returns the amount charged and
// the shortfall.
func SettleOrganizationQuota(organizationId int, userId int, requestId string, seq int, delta int64, now int64) (int64, int64, error) {
	if delta == 0 {
		return 0, 0, nil
	}
	if requestId == "" {
		return 0, 0, ErrOrganizationLedgerRefInvalid
	}
	var charged, shortfall int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		if delta < 0 {
			key, err := organizationLedgerKey(OrganizationLedgerRefund, organizationId, fmt.Sprintf("%s:settle:%d", requestId, seq))
			if err != nil {
				return err
			}
			return refundOrganizationQuotaTx(tx, organizationId, userId, -delta, key, requestId, 0, now)
		}
		account, err := lockOrganizationQuotaAccount(tx, organizationId, now)
		if err != nil {
			return err
		}
		member, err := lockOrganizationMemberQuota(tx, organizationId, userId, now)
		if err != nil {
			return err
		}
		memberRemaining, orgRemaining := organizationRemaining(account, member)
		charged = min(delta, max(memberRemaining, 0), max(orgRemaining, 0))
		shortfall = delta - charged
		if charged > 0 {
			key, err := organizationLedgerKey(OrganizationLedgerSettle, organizationId, fmt.Sprintf("%s:%d", requestId, seq))
			if err != nil {
				return err
			}
			recorded, err := recordOrganizationLedgerEntry(tx, &OrganizationQuotaLedgerEntry{
				OrganizationId: organizationId, EventType: OrganizationLedgerSettle, Amount: charged, TargetUserId: userId,
				RequestId: requestId, IdempotencyKey: key, CreatedTime: now,
			})
			if err != nil {
				return err
			}
			if recorded {
				if err := applyOrganizationUsage(tx, organizationId, userId, charged, now); err != nil {
					return err
				}
			}
		}
		if shortfall > 0 {
			key, err := organizationLedgerKey(OrganizationLedgerShortfall, organizationId, fmt.Sprintf("%s:%d", requestId, seq))
			if err != nil {
				return err
			}
			_, err = recordOrganizationLedgerEntry(tx, &OrganizationQuotaLedgerEntry{
				OrganizationId: organizationId, EventType: OrganizationLedgerShortfall, Amount: shortfall, TargetUserId: userId,
				RequestId: requestId, IdempotencyKey: key, CreatedTime: now,
			})
			return err
		}
		return nil
	})
	return charged, shortfall, err
}

// refundOrganizationQuotaTx returns usage to the organization and the member. The caller
// owns the transaction and holds the organization lock. A refund can never exceed the
// usage it reverses. Refunds are allowed for disabled members, whose usage is kept.
func refundOrganizationQuotaTx(tx *gorm.DB, organizationId int, userId int, amount int64, key string, requestId string, actorUserId int, now int64) error {
	account, err := lockOrganizationQuotaAccount(tx, organizationId, now)
	if err != nil {
		return err
	}
	member, err := lockOrganizationMemberQuota(tx, organizationId, userId, now)
	if err != nil {
		return err
	}
	if member.UsedQuota < amount || account.ConsumedQuota < amount {
		return ErrOrganizationRefundExceeded
	}
	recorded, err := recordOrganizationLedgerEntry(tx, &OrganizationQuotaLedgerEntry{
		OrganizationId: organizationId, EventType: OrganizationLedgerRefund, Amount: amount, TargetUserId: userId,
		ActorUserId: actorUserId, RequestId: requestId, IdempotencyKey: key, CreatedTime: now,
	})
	if err != nil || !recorded {
		return err
	}
	return applyOrganizationUsage(tx, organizationId, userId, -amount, now)
}

// RefundOrganizationPreConsume returns usage still held for a request that ends
// without a settled charge, such as a failed request. The key is fixed per request, so
// retries cannot refund twice.
func RefundOrganizationPreConsume(organizationId int, userId int, requestId string, amount int64, now int64) error {
	if amount <= 0 {
		return nil
	}
	if requestId == "" {
		return ErrOrganizationLedgerRefInvalid
	}
	key, err := organizationLedgerKey(OrganizationLedgerRefund, organizationId, requestId+":final")
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		var replayed int64
		if err := tx.Model(&OrganizationQuotaLedgerEntry{}).Where("idempotency_key = ?", key).Count(&replayed).Error; err != nil {
			return err
		}
		if replayed > 0 {
			return nil
		}
		return refundOrganizationQuotaTx(tx, organizationId, userId, amount, key, requestId, 0, now)
	})
}

// RefundOrganizationRequest is the manual refund of a settled request (D10). It is
// limited to the quota the request still holds, returns to the member who consumed it,
// and is idempotent for the reference. Callers must restrict it to Root.
func RefundOrganizationRequest(organizationId int, requestId string, amount int64, actorUserId int, ref string, now int64) (bool, error) {
	if amount <= 0 {
		return false, ErrOrganizationQuotaInvalidAmount
	}
	if requestId == "" {
		return false, ErrOrganizationLedgerRefInvalid
	}
	key, err := organizationLedgerKey(OrganizationLedgerRefund, organizationId, "manual:"+ref)
	if err != nil {
		return false, err
	}
	applied := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		var replayed int64
		if err := tx.Model(&OrganizationQuotaLedgerEntry{}).Where("idempotency_key = ?", key).Count(&replayed).Error; err != nil {
			return err
		}
		if replayed > 0 {
			return nil
		}
		var preConsume OrganizationQuotaLedgerEntry
		if err := tx.Where("organization_id = ? AND request_id = ? AND event_type = ?", organizationId, requestId, OrganizationLedgerPreConsume).
			Order("id").First(&preConsume).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOrganizationRequestUnknown
			}
			return err
		}
		net, err := organizationRequestNetCharged(tx, organizationId, requestId)
		if err != nil {
			return err
		}
		if amount > net {
			return ErrOrganizationRefundExceeded
		}
		applied = true
		return refundOrganizationQuotaTx(tx, organizationId, preConsume.TargetUserId, amount, key, requestId, actorUserId, now)
	})
	return applied, err
}

// organizationRequestNetCharged sums the quota charged to a request, minus the quota
// already refunded from it, using the ledger as the source of truth.
func organizationRequestNetCharged(tx *gorm.DB, organizationId int, requestId string) (int64, error) {
	var rows []OrganizationQuotaLedgerEntry
	if err := tx.Where("organization_id = ? AND request_id = ? AND event_type IN ?", organizationId, requestId,
		[]string{OrganizationLedgerPreConsume, OrganizationLedgerSettle, OrganizationLedgerRefund}).Find(&rows).Error; err != nil {
		return 0, err
	}
	var net int64
	for _, row := range rows {
		if row.EventType == OrganizationLedgerRefund {
			net -= row.Amount
		} else {
			net += row.Amount
		}
	}
	return net, nil
}
