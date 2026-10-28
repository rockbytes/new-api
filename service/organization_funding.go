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
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// BillingSourceOrganization identifies usage charged to an organization's quota (D2).
// It never falls back to the wallet or a subscription (D3).
const BillingSourceOrganization = "organization"

// ErrInsufficientOrganizationQuota means the organization's remaining quota cannot cover
// the reservation. ErrInsufficientMemberQuota means the member's allocation cannot. Both
// reject the request, and nothing is deducted.
var (
	ErrInsufficientOrganizationQuota = errors.New("insufficient organization quota")
	ErrInsufficientMemberQuota       = errors.New("insufficient member quota")
	// ErrOrganizationMemberInactive means the member is disabled. Usage is blocked, but the allocation and usage are kept.
	ErrOrganizationMemberInactive = errors.New("organization member is disabled")
	// ErrOrganizationInactive means the organization is suspended.
	ErrOrganizationInactive = errors.New("organization is suspended")
)

// OrganizationQuotaFunding charges one request to the organization's quota, within the
// member's allocation. Pre-consume reserves usage; settle applies the final difference;
// refund returns whatever the request still holds.
type OrganizationQuotaFunding struct {
	organizationId int
	userId         int
	requestId      string
	seq            int // per-request sequence so each ledger event has its own idempotency key
	outstanding    int // usage this request currently holds in the organization ledger
}

func (o *OrganizationQuotaFunding) Source() string { return BillingSourceOrganization }

func (o *OrganizationQuotaFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	o.seq++
	err := model.ConsumeOrganizationQuota(o.organizationId, o.userId, o.requestId, o.seq, int64(amount), common.GetTimestamp())
	switch {
	case errors.Is(err, model.ErrOrganizationMemberQuotaInsufficient):
		return ErrInsufficientMemberQuota
	case errors.Is(err, model.ErrOrganizationQuotaInsufficient):
		return ErrInsufficientOrganizationQuota
	case errors.Is(err, model.ErrOrganizationMembershipMissing):
		return ErrOrganizationMemberInactive
	case errors.Is(err, model.ErrOrganizationNotActive):
		return ErrOrganizationInactive
	case err != nil:
		return err
	}
	o.outstanding += amount
	return nil
}

func (o *OrganizationQuotaFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	// A refund can never exceed what this request still holds.
	if delta < 0 && -delta > o.outstanding {
		delta = -o.outstanding
	}
	if delta == 0 {
		return nil
	}
	o.seq++
	charged, shortfall, err := model.SettleOrganizationQuota(o.organizationId, o.userId, o.requestId, o.seq, int64(delta), common.GetTimestamp())
	if err != nil {
		return err
	}
	if shortfall > 0 {
		common.SysLog("organization quota shortfall: request=" + o.requestId + " not charged")
	}
	if delta > 0 {
		o.outstanding += int(charged)
	} else {
		o.outstanding -= -delta
	}
	return nil
}

func (o *OrganizationQuotaFunding) Refund() error {
	if o.outstanding <= 0 {
		return nil
	}
	if err := model.RefundOrganizationPreConsume(o.organizationId, o.userId, o.requestId, int64(o.outstanding), common.GetTimestamp()); err != nil {
		return err
	}
	o.outstanding = 0
	return nil
}
