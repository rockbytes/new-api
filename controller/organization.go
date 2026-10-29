package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
)

const organizationNameMaxLength = 128

type organizationCreateRequest struct {
	Name          string `json:"name"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
}

type organizationNameRequest struct {
	Name string `json:"name"`
}

type organizationStatusRequest struct {
	Status string `json:"status"`
}

type organizationMemberCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type organizationPasswordRequest struct {
	Password string `json:"password"`
}

// organizationMemberUpdateRequest uses pointers so an omitted field is left
// unchanged, while an explicit value is applied.
type organizationMemberUpdateRequest struct {
	Role   *string `json:"role"`
	Status *string `json:"status"`
}

type organizationSelfResponse struct {
	Organization *model.Organization `json:"organization"`
	Role         string              `json:"role"`
	Status       string              `json:"status"`
	Permissions  []string            `json:"permissions"`
}

// writeOrganizationError responds with a stable code and a message key. The web
// console maps code to a translated message; message_key is the English source
// text and is only a fallback.
func writeOrganizationError(c *gin.Context, status int, code string, key string) {
	common.ApiErrorStatus(c, status, common.NewMessage(key), gin.H{"code": code})
}

// respondOrganizationError maps a model error to its HTTP status and code.
// Unknown errors are logged and answered with a generic message, never the
// internal error text.
func respondOrganizationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrOrganizationNotFound):
		writeOrganizationError(c, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "Organization not found")
	case errors.Is(err, model.ErrOrganizationMembershipMissing):
		writeOrganizationError(c, http.StatusNotFound, "ORGANIZATION_MEMBERSHIP_NOT_FOUND", "Organization membership not found")
	case errors.Is(err, model.ErrOrganizationInvalidRole):
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_ROLE", "Invalid organization role")
	case errors.Is(err, model.ErrOrganizationInvalidStatus):
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_STATUS", "Invalid organization membership status")
	case errors.Is(err, model.ErrOrganizationStatusTransition):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_STATUS_TRANSITION_INVALID", "Invalid organization status transition")
	case errors.Is(err, model.ErrOrganizationNotActive):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_NOT_ACTIVE", "Organization is not active")
	case errors.Is(err, model.ErrOrganizationLastAdmin):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_LAST_ADMIN", "Organization must keep at least one active admin")
	case errors.Is(err, model.ErrOrganizationUsernameTaken):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_USERNAME_TAKEN", "Username is already taken")
	case errors.Is(err, model.ErrUserInOrganization):
		writeOrganizationError(c, http.StatusConflict, "USER_IN_ORGANIZATION", "User belongs to an organization; remove the membership first")
	case errors.Is(err, model.ErrOrganizationUnallocatedInsufficient):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_QUOTA_INSUFFICIENT", "Unallocated quota is not enough for this change")
	case errors.Is(err, model.ErrOrganizationAdjustModeInvalid):
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid adjustment mode")
	case errors.Is(err, model.ErrOrganizationQuotaInvalidAmount):
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_QUOTA_INVALID_AMOUNT", "Invalid quota amount")
	case errors.Is(err, model.ErrOrganizationLedgerRefInvalid):
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_REFERENCE_INVALID", "Invalid quota reference")
	case errors.Is(err, model.ErrOrganizationMemberQuotaBelowUsage):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_QUOTA_BELOW_USAGE", "Allocation cannot be below the usage already recorded")
	case errors.Is(err, model.ErrOrganizationMemberRoleNotAllowed):
		writeOrganizationError(c, http.StatusForbidden, "ORGANIZATION_PERMISSION_DENIED", "You do not have permission to perform this action")
	case errors.Is(err, model.ErrOrganizationRefundExceeded):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_REFUND_EXCEEDED", "Refund exceeds the settled quota of the request")
	case errors.Is(err, model.ErrOrganizationRequestUnknown):
		writeOrganizationError(c, http.StatusNotFound, "ORGANIZATION_REQUEST_NOT_FOUND", "No quota was charged for this request")
	case errors.Is(err, model.ErrOrganizationQuotaOverflow):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_QUOTA_OVERFLOW", "Quota total would overflow")
	default:
		common.SysError("organization request failed: " + err.Error())
		writeOrganizationError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func organizationIdParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_ID", "Invalid organization id")
		return 0, false
	}
	return id, true
}

func organizationMemberUserIdParam(c *gin.Context) (int, bool) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_USER_ID", "Invalid user id")
		return 0, false
	}
	return userId, true
}

// normalizeOrganizationName trims the name and reports whether it is acceptable.
func normalizeOrganizationName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && len(name) <= organizationNameMaxLength
}

// organizationInitialPasswordValid applies the account password policy used by
// User.Password (8 to 128 characters) to initial and reset passwords.
func organizationInitialPasswordValid(password string) bool {
	return common.Validate.Var(password, "min=8,max=128") == nil
}

func GetOrganizations(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	organizations, total, err := model.ListOrganizations(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(organizations)
	common.ApiSuccess(c, pageInfo)
}

func GetOrganization(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	organization, err := model.GetOrganizationById(id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	members, err := model.ListOrganizationMemberViews(id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"organization": organization, "members": members})
}

func CreateOrganization(c *gin.Context) {
	var request organizationCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	name, ok := normalizeOrganizationName(request.Name)
	if !ok {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_NAME_REQUIRED", "Organization name is required")
		return
	}
	adminUsername := strings.TrimSpace(request.AdminUsername)
	if adminUsername == "" {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_ADMIN_USERNAME_REQUIRED", "Admin username is required")
		return
	}
	if !organizationInitialPasswordValid(request.AdminPassword) {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_PASSWORD_INVALID", "Password must be 8 to 128 characters")
		return
	}
	organization, err := model.CreateOrganizationWithAdmin(name, adminUsername, request.AdminPassword, c.GetInt("id"), common.GetTimestamp())
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, organization)
}

func UpdateOrganization(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	var request organizationNameRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	name, ok := normalizeOrganizationName(request.Name)
	if !ok {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_NAME_REQUIRED", "Organization name is required")
		return
	}
	if err := model.UpdateOrganizationName(id, name, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UpdateOrganizationStatus(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	var request organizationStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	if request.Status != model.OrganizationStatusActive && request.Status != model.OrganizationStatusSuspended {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_STATUS", "Invalid organization status")
		return
	}
	if err := model.ChangeOrganizationStatus(id, request.Status, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// loadOrganizationCaller resolves the caller's organization from their active
// membership and checks the role grants the permission. The organization ID is
// never read from the request, so it cannot select another organization.
func loadOrganizationCaller(c *gin.Context, permission string) (*model.OrganizationMembership, *model.Organization, bool) {
	membership, organization, err := model.GetActiveOrganizationMembership(c.GetInt("id"))
	if err != nil {
		respondOrganizationError(c, err)
		return nil, nil, false
	}
	if !authz.OrganizationRoleAllows(membership.Role, permission) {
		writeOrganizationError(c, http.StatusForbidden, "ORGANIZATION_PERMISSION_DENIED", "Permission denied")
		return nil, nil, false
	}
	return membership, organization, true
}

func GetOrganizationSelf(c *gin.Context) {
	membership, organization, err := model.GetActiveOrganizationMembership(c.GetInt("id"))
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, organizationSelfResponse{
		Organization: organization,
		Role:         membership.Role,
		Status:       membership.Status,
		Permissions:  authz.OrganizationRolePermissions(membership.Role),
	})
}

func ListOrganizationMembers(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionMembersList)
	if !ok {
		return
	}
	members, err := model.ListOrganizationMemberViews(organization.Id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, members)
}

func CreateOrganizationMember(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionMembersCreate)
	if !ok {
		return
	}
	var request organizationMemberCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	username := strings.TrimSpace(request.Username)
	if username == "" {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_USERNAME_REQUIRED", "Username is required")
		return
	}
	if !organizationInitialPasswordValid(request.Password) {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_PASSWORD_INVALID", "Password must be 8 to 128 characters")
		return
	}
	if err := model.CreateOrganizationMemberAccount(organization.Id, username, request.Password, c.GetInt("id"), common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UpdateOrganizationMember(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionMembersUpdate)
	if !ok {
		return
	}
	userId, ok := organizationMemberUserIdParam(c)
	if !ok {
		return
	}
	var request organizationMemberUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	if err := model.UpdateOrganizationMember(organization.Id, userId, request.Role, request.Status, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ResetOrganizationMemberPassword(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionMembersUpdate)
	if !ok {
		return
	}
	userId, ok := organizationMemberUserIdParam(c)
	if !ok {
		return
	}
	var request organizationPasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_INVALID_REQUEST", "Invalid request")
		return
	}
	if !organizationInitialPasswordValid(request.Password) {
		writeOrganizationError(c, http.StatusBadRequest, "ORGANIZATION_PASSWORD_INVALID", "Password must be 8 to 128 characters")
		return
	}
	if err := model.ResetOrganizationMemberPassword(organization.Id, userId, request.Password, common.GetTimestamp()); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DeleteOrganizationMember(c *gin.Context) {
	_, organization, ok := loadOrganizationCaller(c, authz.OrganizationPermissionMembersRemove)
	if !ok {
		return
	}
	userId, ok := organizationMemberUserIdParam(c)
	if !ok {
		return
	}
	if err := model.DeleteOrganizationMember(organization.Id, userId); err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetOrganizationDeletionImpact lists what deleting the organization would touch and
// which preconditions are still unmet. It changes nothing.
func GetOrganizationDeletionImpact(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	impact, err := model.GetOrganizationDeletionImpact(id)
	if err != nil {
		respondOrganizationError(c, err)
		return
	}
	common.ApiSuccess(c, impact)
}

// DeleteOrganization soft-deletes a suspended organization once its dependencies are
// cleared. A blocked deletion returns the impact so the caller can see what remains.
func DeleteOrganization(c *gin.Context) {
	id, ok := organizationIdParam(c)
	if !ok {
		return
	}
	err := model.DeleteOrganization(id, common.GetTimestamp())
	switch {
	case errors.Is(err, model.ErrOrganizationDeleteNotSuspended):
		writeOrganizationError(c, http.StatusConflict, "ORGANIZATION_DELETE_NOT_SUSPENDED", "Suspend the organization before deleting it")
		return
	case errors.Is(err, model.ErrOrganizationDeleteBlocked):
		impact, impactErr := model.GetOrganizationDeletionImpact(id)
		if impactErr != nil {
			respondOrganizationError(c, impactErr)
			return
		}
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "Clear the listed dependencies before deleting the organization",
			"code":    "ORGANIZATION_DELETE_BLOCKED",
			"data":    impact,
		})
		return
	case err != nil:
		respondOrganizationError(c, err)
		return
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId:   c.GetInt("id"),
		Category: model.AuditCategoryOperation,
		Action:   "organization.delete",
		Success:  true,
		Content:  fmt.Sprintf("organization %d soft-deleted", id),
	})
	common.ApiSuccess(c, nil)
}
