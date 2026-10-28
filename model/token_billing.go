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

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ErrTokenBillingOrganizationNotMember means the Key owner is not an active member of
// the organization they tried to bind the Key to.
var ErrTokenBillingOrganizationNotMember = errors.New("only an active member of the organization can bind a key to it")

// IsActiveOrganizationMember reports whether the user is an active member of an active
// organization. A missing organization or membership is not an error.
func IsActiveOrganizationMember(organizationId int, userId int) (bool, error) {
	organization, err := GetOrganizationById(organizationId)
	if errors.Is(err, ErrOrganizationNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if organization.Status != OrganizationStatusActive {
		return false, nil
	}
	membership, err := getOrganizationMemberTx(DB, organizationId, userId)
	if errors.Is(err, ErrOrganizationMembershipMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return membership.Status == OrganizationMembershipActive, nil
}

// BindTokenBillingOrganization sets or clears the paying organization of a Key. Only the
// owner can change it, and only to an organization where the owner is an active member
// (D2 binding rule). Binding a company-paid Key also turns off user quota queries, so the
// company balance is not exposed through the Key. The change applies to later requests
// only; logs keep the organization that was recorded when they were written.
func BindTokenBillingOrganization(tokenId int, userId int, organizationId *int) (*Token, error) {
	token, err := GetTokenByIds(tokenId, userId)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"billing_organization_id": organizationId}
	if organizationId != nil {
		member, err := IsActiveOrganizationMember(*organizationId, userId)
		if err != nil {
			return nil, err
		}
		if !member {
			return nil, ErrTokenBillingOrganizationNotMember
		}
		updates["allow_user_quota_query"] = false
	}
	if err := DB.Model(&Token{}).Where("id = ? AND user_id = ?", tokenId, userId).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := invalidateTokenCacheForMutation(token.Key); err != nil {
		return nil, err
	}
	token.BillingOrganizationId = organizationId
	if organizationId != nil {
		token.AllowUserQuotaQuery = false
	}
	return token, nil
}

// BillingOrganizationIdValue returns the organization that pays for requests made with
// this Key, or zero when the owner pays from their own wallet or subscription.
func (token *Token) BillingOrganizationIdValue() int {
	if token == nil || token.BillingOrganizationId == nil {
		return 0
	}
	return *token.BillingOrganizationId
}

// disableOrganizationPaidTokensTx disables the Keys a member bound to an organization when
// the member is disabled or removed (D4). Keys are disabled, not moved to personal billing.
// It returns the affected keys so the caller can drop cached copies after commit.
func disableOrganizationPaidTokensTx(tx *gorm.DB, organizationId int, userId int) ([]string, error) {
	var tokens []Token
	if err := tx.
		Where("user_id = ? AND billing_organization_id = ? AND status <> ?", userId, organizationId, common.TokenStatusDisabled).
		Find(&tokens).Error; err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	if err := tx.Model(&Token{}).Where("user_id = ? AND billing_organization_id = ?", userId, organizationId).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(tokens))
	for _, token := range tokens {
		keys = append(keys, token.Key)
	}
	return keys, nil
}

// disableAllOrganizationPaidTokensTx disables every Key bound to the organization, whichever
// member owns it. It is used when the organization is suspended. The Keys stay bound.
func disableAllOrganizationPaidTokensTx(tx *gorm.DB, organizationId int) ([]string, error) {
	var tokens []Token
	if err := tx.
		Where("billing_organization_id = ? AND status <> ?", organizationId, common.TokenStatusDisabled).
		Find(&tokens).Error; err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	if err := tx.Model(&Token{}).Where("billing_organization_id = ?", organizationId).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(tokens))
	for _, token := range tokens {
		keys = append(keys, token.Key)
	}
	return keys, nil
}

// invalidateTokenKeys drops cached copies of Keys whose database rows changed.
func invalidateTokenKeys(keys []string) {
	for _, key := range keys {
		if err := invalidateTokenCacheForMutation(key); err != nil {
			common.SysError("invalidate token cache after organization member change: " + err.Error())
		}
	}
}
