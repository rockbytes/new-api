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

package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type tokenBillingOrganizationRequest struct {
	// OrganizationId is the organization that pays for this Key, or null for the owner's own wallet.
	OrganizationId *int `json:"organization_id"`
}

// UpdateTokenBillingOrganization binds a Key to the organization that pays for it, or
// clears the binding. Only the Key owner can call it, and only with an organization where
// the owner is an active member. The organization is taken from the body only.
func UpdateTokenBillingOrganization(c *gin.Context) {
	tokenId, err := strconv.Atoi(c.Param("id"))
	if err != nil || tokenId <= 0 {
		common.ApiErrorT(c, "Invalid token id")
		return
	}
	var request tokenBillingOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.OrganizationId != nil && *request.OrganizationId <= 0 {
		common.ApiErrorT(c, "Invalid organization id")
		return
	}
	token, err := model.BindTokenBillingOrganization(tokenId, c.GetInt("id"), request.OrganizationId)
	if errors.Is(err, model.ErrTokenBillingOrganizationNotMember) {
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Only an active member of the organization can bind a key to it"),
			gin.H{"code": "TOKEN_BILLING_ORGANIZATION_FORBIDDEN"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"id":                      token.Id,
		"billing_organization_id": token.BillingOrganizationId,
		"allow_user_quota_query":  token.AllowUserQuotaQuery,
	})
}
