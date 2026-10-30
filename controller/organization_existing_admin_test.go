package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The create route accepts an existing account as the first admin. An ineligible account is
// refused with a stable code and no organization is created.
func TestOrganizationCreateWithExistingAdminUser(t *testing.T) {
	db := setupOrganizationTestDB(t)
	candidateId := createOrganizationTestUser(t, db, "candidate-admin", common.UserStatusEnabled)
	platformAdmin := &model.User{Username: "platform-admin", Password: "not-a-real-password", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, AffCode: "aff-platform-admin"}
	require.NoError(t, db.Create(platformAdmin).Error)

	recorder := doOrganizationRequest(t, organizationTestRootId, http.MethodPost, "/api/organization/",
		fmt.Sprintf(`{"name":"Existing Admin Co","admin_user_id":%d}`, candidateId))
	data, status := organizationResponseData(t, recorder)
	require.Equal(t, http.StatusOK, status, recorder.Body.String())
	var organization model.Organization
	require.NoError(t, common.Unmarshal(data, &organization))

	var membership model.OrganizationMembership
	require.NoError(t, db.Where("user_id = ?", candidateId).First(&membership).Error)
	assert.Equal(t, organization.Id, membership.OrganizationId)
	assert.Equal(t, model.OrganizationRoleAdmin, membership.Role)

	recorder = doOrganizationRequest(t, organizationTestRootId, http.MethodPost, "/api/organization/",
		fmt.Sprintf(`{"name":"Refused Co","admin_user_id":%d}`, platformAdmin.Id))
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "ORGANIZATION_ADMIN_NOT_ELIGIBLE", organizationErrorCode(t, recorder))

	var refused int64
	require.NoError(t, db.Model(&model.Organization{}).Where("name = ?", "Refused Co").Count(&refused).Error)
	assert.Equal(t, int64(0), refused)
}
