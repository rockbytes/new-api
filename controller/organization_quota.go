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
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

const organizationQuotaReasonMaxLength = 255

type organizationGrantRequest struct {
	Amount    int64  `json:"amount"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

type organizationAllocationRequest struct {
	Allocated int64 `json:"allocated"`
}

type organizationRefundRequest struct {
	Amount    int64  `json:"amount"`
	Reference string `json:"reference"`
}

// RootGetOrganizationQuota returns the quota totals and the reconciliation of an organization.
func RootGetOrganizationQuota(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	reconciliation, err := model.GetOrganizationQuotaReconciliation(id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, reconciliation)
}

// RootGrantOrganizationQuota records a platform grant that raises the organization's total (D8).
func RootGrantOrganizationQuota(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	var req organizationGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Reason) > organizationQuotaReasonMaxLength {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	applied, err := model.GrantOrganizationQuota(id, req.Amount, c.GetInt("id"), req.Reason, req.Reference, common.GetTimestamp())
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"applied": applied})
}

// RootSetOrganizationMemberAllocation sets any member's allocation, including organization admins'.
func RootSetOrganizationMemberAllocation(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	userId, ok := organizationMemberUserIdParam(c)
	if !ok {
		return
	}
	var req organizationAllocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	if err := model.SetOrganizationMemberAllocation(id, userId, req.Allocated, false, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// RootRefundOrganizationRequest is the manual refund of a settled request (D10). Root only.
func RootRefundOrganizationRequest(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	requestId := c.Param("request_id")
	var req organizationRefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	applied, err := model.RefundOrganizationRequest(id, requestId, req.Amount, c.GetInt("id"), req.Reference, common.GetTimestamp())
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"applied": applied})
}

// GetOwnOrganizationQuota returns the organization's quota totals to its members.
func GetOwnOrganizationQuota(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionView)
	if !ok {
		return
	}
	reconciliation, err := model.GetOrganizationQuotaReconciliation(organization.Id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, reconciliation)
}

// SetOwnOrganizationMemberAllocation lets an organization admin set the allocation of an
// ordinary member. Admins cannot set their own or other admins' allocations, which the
// model enforces inside the same transaction as the write.
func SetOwnOrganizationMemberAllocation(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionQuotaUpdate)
	if !ok {
		return
	}
	userId, ok := organizationMemberUserIdParam(c)
	if !ok {
		return
	}
	var req organizationAllocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	if err := model.SetOrganizationMemberAllocation(organization.Id, userId, req.Allocated, true, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type organizationAdjustRequest struct {
	Mode      string `json:"mode"`
	Amount    int64  `json:"amount"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

// RootAdjustOrganizationQuota adds to, subtracts from, or overrides the organization's
// unallocated quota. Platform administrators and Root may use it.
func RootAdjustOrganizationQuota(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	var req organizationAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Reason) > organizationQuotaReasonMaxLength {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	result, err := model.AdjustOrganizationUnallocatedQuota(id, req.Mode, req.Amount, c.GetInt("id"), req.Reason, req.Reference, common.GetTimestamp())
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
