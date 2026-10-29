package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
)

// Organization statuses. An organization is usable as soon as Root creates it
// together with its first admin account (D11); there is no pending state.
const (
	OrganizationStatusActive    = "active"
	OrganizationStatusSuspended = "suspended"
	OrganizationStatusDeleted   = "deleted"
)

// Organization membership roles and statuses. A disabled membership keeps the
// account and its usage attribution, and blocks login through the account
// status (D12). Deleting a member removes the account and the membership row.
const (
	OrganizationRoleAdmin  = "admin"
	OrganizationRoleMember = "member"

	OrganizationMembershipActive   = "active"
	OrganizationMembershipDisabled = "disabled"
)

// organizationStatusTransitions lists the status changes Root may request.
var organizationStatusTransitions = map[string][]string{
	OrganizationStatusActive:    {OrganizationStatusSuspended},
	OrganizationStatusSuspended: {OrganizationStatusActive},
}

var (
	ErrOrganizationNotFound          = errors.New("organization not found")
	ErrOrganizationMembershipMissing = errors.New("organization membership not found")
	ErrOrganizationStatusTransition  = errors.New("invalid organization status transition")
	ErrOrganizationNotActive         = errors.New("organization is not active")
	ErrOrganizationLastAdmin         = errors.New("organization must keep at least one active admin")
	ErrOrganizationUsernameTaken     = errors.New("username is already taken")
	ErrOrganizationInvalidRole       = errors.New("invalid organization role")
	ErrOrganizationInvalidStatus     = errors.New("invalid organization membership status")
	ErrUserInOrganization            = errors.New("user belongs to an organization; remove the membership first")
)

type Organization struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"type:varchar(128);index"`
	Status      string `json:"status" gorm:"type:varchar(32);index"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

type OrganizationMembership struct {
	Id             int    `json:"id"`
	OrganizationId int    `json:"organization_id" gorm:"not null;index:idx_org_membership_org_status,priority:1"`
	UserId         int    `json:"user_id" gorm:"not null;uniqueIndex"`
	Role           string `json:"role" gorm:"type:varchar(16);not null"`
	Status         string `json:"status" gorm:"type:varchar(16);not null;index:idx_org_membership_org_status,priority:2"`
	InvitedBy      int    `json:"invited_by"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64  `json:"updated_time" gorm:"bigint"`
}

// OrganizationMemberView is a membership with the member's account name, for
// responses to organization admins and Root.
type OrganizationMemberView struct {
	UserId         int    `json:"user_id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	CreatedTime    int64  `json:"created_time"`
	AllocatedQuota int64  `json:"allocated_quota"`
	UsedQuota      int64  `json:"used_quota"`
}

func ValidOrganizationRole(role string) bool {
	return role == OrganizationRoleAdmin || role == OrganizationRoleMember
}

func CanChangeOrganizationStatus(from string, to string) bool {
	return slices.Contains(organizationStatusTransitions[from], to)
}

// lockOrganization serializes membership and status changes for one organization.
// Every write that can change the active-admin count takes this lock first, so
// concurrent demotions, disables or deletes cannot leave zero active admins.
func lockOrganization(tx *gorm.DB, organizationId int) (*Organization, error) {
	var organization Organization
	err := lockForUpdate(tx).Where("id = ?", organizationId).First(&organization).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrganizationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &organization, nil
}

func countActiveOrganizationAdmins(tx *gorm.DB, organizationId int) (int64, error) {
	var count int64
	err := tx.Model(&OrganizationMembership{}).
		Where("organization_id = ? AND role = ? AND status = ?", organizationId, OrganizationRoleAdmin, OrganizationMembershipActive).
		Count(&count).Error
	return count, err
}

// createOrganizationAccountWithTx creates an enabled common account that must
// change its initial password at first login (D12). It mirrors the parts of
// User.Insert that apply to a password-created account, inside the caller's
// transaction so the account and its membership are created atomically.
func createOrganizationAccountWithTx(tx *gorm.DB, username string, password string) (*User, error) {
	username = strings.TrimSpace(username)
	var existing int64
	if err := tx.Model(&User{}).Where("username = ?", username).Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrOrganizationUsernameTaken
	}
	hashed, err := common.HashAccountPassword(password)
	if err != nil {
		return nil, err
	}
	setting := dto.UserSetting{SidebarModules: generateDefaultSidebarConfigForRole(common.RoleCommonUser)}
	user := &User{
		Username:           username,
		Password:           hashed,
		Role:               common.RoleCommonUser,
		Status:             common.UserStatusEnabled,
		MustChangePassword: true,
		Quota:              common.QuotaForNewUser,
		AffCode:            common.GetRandomString(4),
	}
	user.SetSetting(setting)
	if err := tx.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// userAuthUpdate is the work that must run after a transaction changes a user's
// credentials or status: publish the new auth version and drop cached entries.
type userAuthUpdate struct {
	userId      int
	authVersion int64
}

func (update userAuthUpdate) finish() {
	if err := publishCommittedUserAuthVersion(update.userId, update.authVersion); err != nil {
		common.SysError(common.LogText("failed to publish auth version for user %d: %v", update.userId, err))
	}
	if err := invalidateUserCache(update.userId); err != nil {
		common.SysError(common.LogText("failed to invalidate user cache for user %d: %v", update.userId, err))
	}
}

func CreateOrganizationWithAdmin(name string, adminUsername string, adminPassword string, operatorId int, now int64) (*Organization, error) {
	organization := &Organization{Name: name, Status: OrganizationStatusActive, CreatedTime: now, UpdatedTime: now}
	err := DB.Transaction(func(tx *gorm.DB) error {
		admin, err := createOrganizationAccountWithTx(tx, adminUsername, adminPassword)
		if err != nil {
			return err
		}
		if err := tx.Create(organization).Error; err != nil {
			return err
		}
		return tx.Create(&OrganizationMembership{
			OrganizationId: organization.Id,
			UserId:         admin.Id,
			Role:           OrganizationRoleAdmin,
			Status:         OrganizationMembershipActive,
			InvitedBy:      operatorId,
			CreatedTime:    now,
			UpdatedTime:    now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return organization, nil
}

func GetOrganizationById(id int) (*Organization, error) {
	var organization Organization
	err := DB.Where("id = ?", id).First(&organization).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrganizationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &organization, nil
}

func ListOrganizations(startIdx int, pageSize int) ([]*Organization, int64, error) {
	var total int64
	if err := DB.Model(&Organization{}).Where("status <> ?", OrganizationStatusDeleted).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var organizations []*Organization
	err := DB.Where("status <> ?", OrganizationStatusDeleted).Order("id desc").Limit(pageSize).Offset(startIdx).Find(&organizations).Error
	return organizations, total, err
}

func UpdateOrganizationName(id int, name string, now int64) error {
	result := DB.Model(&Organization{}).Where("id = ?", id).Updates(map[string]any{"name": name, "updated_time": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOrganizationNotFound
	}
	return nil
}

// ChangeOrganizationStatus applies a Root-requested suspend or resume. Resuming
// requires an active admin, so an organization cannot be usable without one.
// Suspending disables the Keys members bound to the organization; resuming does not
// re-enable them.
func ChangeOrganizationStatus(id int, to string, now int64) error {
	var disabledKeys []string
	err := DB.Transaction(func(tx *gorm.DB) error {
		organization, err := lockOrganization(tx, id)
		if err != nil {
			return err
		}
		if !CanChangeOrganizationStatus(organization.Status, to) {
			return ErrOrganizationStatusTransition
		}
		if to == OrganizationStatusActive {
			admins, err := countActiveOrganizationAdmins(tx, id)
			if err != nil {
				return err
			}
			if admins == 0 {
				return ErrOrganizationLastAdmin
			}
		}
		if err := tx.Model(&Organization{}).Where("id = ?", id).Updates(map[string]any{"status": to, "updated_time": now}).Error; err != nil {
			return err
		}
		if to == OrganizationStatusSuspended {
			keys, err := disableAllOrganizationPaidTokensTx(tx, id)
			if err != nil {
				return err
			}
			disabledKeys = keys
		}
		return nil
	})
	if err != nil {
		return err
	}
	invalidateTokenKeys(disabledKeys)
	return nil
}

func ListOrganizationMemberViews(organizationId int) ([]OrganizationMemberView, error) {
	var memberships []OrganizationMembership
	if err := DB.Where("organization_id = ?", organizationId).Order("id asc").Find(&memberships).Error; err != nil {
		return nil, err
	}
	userIds := make([]int, 0, len(memberships))
	for _, membership := range memberships {
		userIds = append(userIds, membership.UserId)
	}
	var users []User
	if len(userIds) > 0 {
		if err := DB.Select("id", "username", "display_name").Where("id IN ?", userIds).Find(&users).Error; err != nil {
			return nil, err
		}
	}
	usersById := make(map[int]User, len(users))
	for _, user := range users {
		usersById[user.Id] = user
	}
	var quotas []OrganizationMemberQuota
	if err := DB.Where("organization_id = ?", organizationId).Find(&quotas).Error; err != nil {
		return nil, err
	}
	quotasByUser := make(map[int]OrganizationMemberQuota, len(quotas))
	for _, quota := range quotas {
		quotasByUser[quota.UserId] = quota
	}
	views := make([]OrganizationMemberView, 0, len(memberships))
	for _, membership := range memberships {
		user := usersById[membership.UserId]
		quota := quotasByUser[membership.UserId]
		views = append(views, OrganizationMemberView{
			UserId:         membership.UserId,
			Username:       user.Username,
			DisplayName:    user.DisplayName,
			Role:           membership.Role,
			Status:         membership.Status,
			CreatedTime:    membership.CreatedTime,
			AllocatedQuota: quota.AllocatedQuota,
			UsedQuota:      quota.UsedQuota,
		})
	}
	return views, nil
}

// GetActiveOrganizationMembership returns the caller's active membership and its
// organization. Write paths must still check the organization status.
func GetActiveOrganizationMembership(userId int) (*OrganizationMembership, *Organization, error) {
	var membership OrganizationMembership
	err := DB.Where("user_id = ? AND status = ?", userId, OrganizationMembershipActive).First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrOrganizationMembershipMissing
	}
	if err != nil {
		return nil, nil, err
	}
	organization, err := GetOrganizationById(membership.OrganizationId)
	if err != nil {
		return nil, nil, err
	}
	return &membership, organization, nil
}

// CreateOrganizationMemberAccount creates a member account with an initial
// password (D12) and its active membership in one transaction.
func CreateOrganizationMemberAccount(organizationId int, username string, password string, operatorId int, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		organization, err := lockOrganization(tx, organizationId)
		if err != nil {
			return err
		}
		if organization.Status != OrganizationStatusActive {
			return ErrOrganizationNotActive
		}
		member, err := createOrganizationAccountWithTx(tx, username, password)
		if err != nil {
			return err
		}
		return tx.Create(&OrganizationMembership{
			OrganizationId: organizationId,
			UserId:         member.Id,
			Role:           OrganizationRoleMember,
			Status:         OrganizationMembershipActive,
			InvitedBy:      operatorId,
			CreatedTime:    now,
			UpdatedTime:    now,
		}).Error
	})
}

func getOrganizationMemberTx(tx *gorm.DB, organizationId int, userId int) (*OrganizationMembership, error) {
	var membership OrganizationMembership
	err := tx.Where("organization_id = ? AND user_id = ?", organizationId, userId).First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrganizationMembershipMissing
	}
	if err != nil {
		return nil, err
	}
	return &membership, nil
}

// UpdateOrganizationMember changes a member's role or enabled state. Disabling a
// member also disables the account and revokes its sessions, so the member cannot
// log in or use the API (D12). Demoting or disabling the last active admin is refused.
func UpdateOrganizationMember(organizationId int, userId int, role *string, status *string, now int64) error {
	var update *userAuthUpdate
	var disabledKeys []string
	err := DB.Transaction(func(tx *gorm.DB) error {
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
		nextRole := membership.Role
		if role != nil {
			if !ValidOrganizationRole(*role) {
				return ErrOrganizationInvalidRole
			}
			nextRole = *role
		}
		nextStatus := membership.Status
		if status != nil {
			if *status != OrganizationMembershipActive && *status != OrganizationMembershipDisabled {
				return ErrOrganizationInvalidStatus
			}
			nextStatus = *status
		}
		wasActiveAdmin := membership.Role == OrganizationRoleAdmin && membership.Status == OrganizationMembershipActive
		losesAdmin := nextRole != OrganizationRoleAdmin || nextStatus != OrganizationMembershipActive
		if wasActiveAdmin && losesAdmin {
			admins, err := countActiveOrganizationAdmins(tx, organizationId)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return ErrOrganizationLastAdmin
			}
		}
		if err := tx.Model(&OrganizationMembership{}).Where("id = ?", membership.Id).
			Updates(map[string]any{"role": nextRole, "status": nextStatus, "updated_time": now}).Error; err != nil {
			return err
		}
		if nextStatus == membership.Status {
			return nil
		}
		if nextStatus == OrganizationMembershipDisabled {
			keys, err := disableOrganizationPaidTokensTx(tx, organizationId, userId)
			if err != nil {
				return err
			}
			disabledKeys = keys
		}
		accountStatus := common.UserStatusEnabled
		if nextStatus == OrganizationMembershipDisabled {
			accountStatus = common.UserStatusDisabled
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).Update("status", accountStatus).Error; err != nil {
			return err
		}
		authVersion, err := IncrementUserAuthVersionWithTx(tx, userId)
		if err != nil {
			return err
		}
		update = &userAuthUpdate{userId: userId, authVersion: authVersion}
		return nil
	})
	if err != nil {
		return err
	}
	invalidateTokenKeys(disabledKeys)
	if update != nil {
		update.finish()
	}
	return nil
}

// ResetOrganizationMemberPassword sets a new initial password for a member of the
// organization and requires a change at the member's next login (D13). The
// member's existing sessions are revoked.
func ResetOrganizationMemberPassword(organizationId int, userId int, password string, now int64) error {
	var update *userAuthUpdate
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		if _, err := getOrganizationMemberTx(tx, organizationId, userId); err != nil {
			return err
		}
		hashed, err := common.HashAccountPassword(password)
		if err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]any{
			"password":             hashed,
			"must_change_password": true,
		}).Error; err != nil {
			return err
		}
		authVersion, err := IncrementUserAuthVersionWithTx(tx, userId)
		if err != nil {
			return err
		}
		update = &userAuthUpdate{userId: userId, authVersion: authVersion}
		return nil
	})
	if err != nil {
		return err
	}
	update.finish()
	return nil
}

// DeleteOrganizationMember deletes a member's account and membership in one
// transaction. This is the only way a member leaves an organization (D12); the
// member's historical usage keeps its consumption-time organization snapshot.
func DeleteOrganizationMember(organizationId int, userId int) error {
	var result hardDeleteResult
	var disabledKeys []string
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOrganization(tx, organizationId); err != nil {
			return err
		}
		membership, err := getOrganizationMemberTx(tx, organizationId, userId)
		if err != nil {
			return err
		}
		if membership.Role == OrganizationRoleAdmin && membership.Status == OrganizationMembershipActive {
			admins, err := countActiveOrganizationAdmins(tx, organizationId)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return ErrOrganizationLastAdmin
			}
		}
		keys, err := disableOrganizationPaidTokensTx(tx, organizationId, userId)
		if err != nil {
			return err
		}
		disabledKeys = keys
		if err := tx.Delete(&OrganizationMembership{}, membership.Id).Error; err != nil {
			return err
		}
		user := &User{Id: userId}
		result, err = user.hardDeleteWithTx(tx)
		return err
	})
	if err != nil {
		return err
	}
	invalidateTokenKeys(disabledKeys)
	result.finish(userId)
	return nil
}

// ensureUserNotInOrganization blocks account deletion through the Root and
// self-service paths while the user holds a membership. Organization admins
// delete members through DeleteOrganizationMember instead.
func ensureUserNotInOrganization(tx *gorm.DB, userId int) error {
	var count int64
	if err := tx.Model(&OrganizationMembership{}).Where("user_id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrUserInOrganization
	}
	return nil
}

var (
	ErrOrganizationDeleteNotSuspended = errors.New("organization must be suspended before deletion")
	ErrOrganizationDeleteBlocked      = errors.New("organization still has dependencies that must be cleared")
)

// OrganizationDeletionImpact is what a deletion would touch. Blockers lists the
// preconditions that are not yet met; the organization can be deleted when it is empty.
type OrganizationDeletionImpact struct {
	OrganizationId   int      `json:"organization_id"`
	Status           string   `json:"status"`
	MemberCount      int64    `json:"member_count"`
	AdminCount       int64    `json:"admin_count"`
	AllocatedQuota   int64    `json:"allocated_quota"`
	UsedQuota        int64    `json:"used_quota"`
	UnallocatedQuota int64    `json:"unallocated_quota"`
	EnabledPaidKeys  int64    `json:"enabled_paid_keys"`
	LedgerEntries    int64    `json:"ledger_entries"`
	ConsumeLogs      int64    `json:"consume_logs"`
	Tasks            int64    `json:"tasks"`
	Blockers         []string `json:"blockers"`
}

// collectOrganizationDeletionImpact reads the dependencies of one organization on the
// given connection. Consume logs live in the log database and are counted by the caller.
func collectOrganizationDeletionImpact(db *gorm.DB, organizationId int, status string) (*OrganizationDeletionImpact, error) {
	impact := &OrganizationDeletionImpact{OrganizationId: organizationId, Status: status}
	if err := db.Model(&OrganizationMembership{}).
		Where("organization_id = ? AND status = ? AND role <> ?", organizationId, OrganizationMembershipActive, OrganizationRoleAdmin).
		Count(&impact.MemberCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&OrganizationMembership{}).
		Where("organization_id = ? AND status = ? AND role = ?", organizationId, OrganizationMembershipActive, OrganizationRoleAdmin).
		Count(&impact.AdminCount).Error; err != nil {
		return nil, err
	}
	var allocatedUsage struct {
		Allocated int64
		Used      int64
	}
	if err := db.Model(&OrganizationMemberQuota{}).
		Select("COALESCE(SUM(allocated_quota), 0) AS allocated, COALESCE(SUM(used_quota), 0) AS used").
		Where("organization_id = ?", organizationId).
		Scan(&allocatedUsage).Error; err != nil {
		return nil, err
	}
	impact.AllocatedQuota = allocatedUsage.Allocated
	impact.UsedQuota = allocatedUsage.Used
	var account OrganizationQuotaAccount
	err := db.Where("organization_id = ?", organizationId).First(&account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// Unallocated = remaining organization quota minus what members still hold.
	remaining := account.GrantedQuota - account.ConsumedQuota
	heldByMembers := impact.AllocatedQuota - impact.UsedQuota
	impact.UnallocatedQuota = remaining - heldByMembers
	if err := db.Model(&Token{}).
		Where("billing_organization_id = ? AND status = ?", organizationId, common.TokenStatusEnabled).
		Count(&impact.EnabledPaidKeys).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&OrganizationQuotaLedgerEntry{}).
		Where("organization_id = ?", organizationId).
		Count(&impact.LedgerEntries).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&Task{}).
		Where("billing_organization_id = ?", organizationId).
		Count(&impact.Tasks).Error; err != nil {
		return nil, err
	}
	impact.Blockers = organizationDeletionBlockers(impact)
	return impact, nil
}

func organizationDeletionBlockers(impact *OrganizationDeletionImpact) []string {
	var blockers []string
	if impact.Status != OrganizationStatusSuspended {
		blockers = append(blockers, "status_not_suspended")
	}
	if impact.MemberCount > 0 {
		blockers = append(blockers, "members_remaining")
	}
	if impact.UnallocatedQuota != 0 {
		blockers = append(blockers, "unallocated_quota_remaining")
	}
	if impact.EnabledPaidKeys > 0 {
		blockers = append(blockers, "enabled_paid_keys")
	}
	return blockers
}

// GetOrganizationDeletionImpact reports the dependencies and blockers of deleting an
// organization. It changes nothing.
func GetOrganizationDeletionImpact(id int) (*OrganizationDeletionImpact, error) {
	organization, err := GetOrganizationById(id)
	if err != nil {
		return nil, err
	}
	impact, err := collectOrganizationDeletionImpact(DB, id, organization.Status)
	if err != nil {
		return nil, err
	}
	if LOG_DB != nil {
		if err := LOG_DB.Model(&Log{}).Where("billing_organization_id = ?", id).Count(&impact.ConsumeLogs).Error; err != nil {
			return nil, err
		}
	}
	return impact, nil
}

// DeleteOrganization soft-deletes an organization that is suspended and has no remaining
// dependencies. The organization row and its history stay in place with status deleted.
func DeleteOrganization(id int, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		organization, err := lockOrganization(tx, id)
		if err != nil {
			return err
		}
		if organization.Status != OrganizationStatusSuspended {
			return ErrOrganizationDeleteNotSuspended
		}
		impact, err := collectOrganizationDeletionImpact(tx, id, organization.Status)
		if err != nil {
			return err
		}
		if len(impact.Blockers) > 0 {
			return ErrOrganizationDeleteBlocked
		}
		return tx.Model(&Organization{}).Where("id = ?", id).
			Updates(map[string]any{"status": OrganizationStatusDeleted, "updated_time": now}).Error
	})
}
