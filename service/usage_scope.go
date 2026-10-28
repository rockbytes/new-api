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

// UsageView is the audience of a usage report. Each view has one scope rule.
type UsageView string

const (
	// UsageViewPlatform covers every organization and user. Platform administrators only.
	UsageViewPlatform UsageView = "platform"
	// UsageViewOrganization covers one organization's paid usage. Platform administrators
	// name the organization; organization administrators get their own.
	UsageViewOrganization UsageView = "organization"
	// UsageViewSelf covers the caller's own usage, whoever paid for it.
	UsageViewSelf UsageView = "self"
)

var (
	ErrUsageViewForbidden        = errors.New("usage view is not available to this caller")
	ErrUsageOrganizationRequired = errors.New("select an organization before selecting a user")
	ErrUsageScopeMismatch        = errors.New("the requested organization or user is outside the caller's scope")
	ErrUsageOrganizationNotFound = errors.New("organization not found")
)

// UsageScopeRequest carries what the caller asked for. The caller's identity is passed
// separately and always wins: the request can only narrow the scope, never widen it.
type UsageScopeRequest struct {
	View           UsageView
	OrganizationId int
	UserId         int
}

// ResolvedUsageScope is the authorized scope of one report.
type ResolvedUsageScope struct {
	View         UsageView
	Scope        model.UsageScope
	Organization *model.Organization // nil when the scope spans organizations
}

// ResolveUsageScope authorizes a usage report for the caller. An empty view defaults to the
// broadest view the caller may see: platform, then the caller's organization, then self.
func ResolveUsageScope(callerId int, callerRole int, request UsageScopeRequest) (ResolvedUsageScope, error) {
	isPlatform := callerRole >= common.RoleAdminUser
	view := request.View
	if view == "" {
		view = defaultUsageView(callerId, isPlatform)
	}
	switch view {
	case UsageViewPlatform:
		if !isPlatform {
			return ResolvedUsageScope{}, ErrUsageViewForbidden
		}
		if request.UserId > 0 && request.OrganizationId == 0 {
			return ResolvedUsageScope{}, ErrUsageOrganizationRequired
		}
		return platformUsageScope(UsageViewPlatform, request)
	case UsageViewOrganization:
		if isPlatform {
			if request.OrganizationId == 0 {
				return ResolvedUsageScope{}, ErrUsageOrganizationRequired
			}
			return platformUsageScope(UsageViewOrganization, request)
		}
		membership, organization, err := model.GetActiveOrganizationMembership(callerId)
		if err != nil || membership.Role != model.OrganizationRoleAdmin || organization.Status != model.OrganizationStatusActive {
			return ResolvedUsageScope{}, ErrUsageViewForbidden
		}
		if request.OrganizationId != 0 && request.OrganizationId != organization.Id {
			return ResolvedUsageScope{}, ErrUsageScopeMismatch
		}
		return ResolvedUsageScope{
			View:         UsageViewOrganization,
			Scope:        model.UsageScope{OrganizationId: organization.Id, UserId: request.UserId},
			Organization: organization,
		}, nil
	case UsageViewSelf:
		if request.OrganizationId != 0 || (request.UserId != 0 && request.UserId != callerId) {
			return ResolvedUsageScope{}, ErrUsageScopeMismatch
		}
		return ResolvedUsageScope{View: UsageViewSelf, Scope: model.UsageScope{UserId: callerId}}, nil
	default:
		return ResolvedUsageScope{}, ErrUsageViewForbidden
	}
}

func defaultUsageView(callerId int, isPlatform bool) UsageView {
	if isPlatform {
		return UsageViewPlatform
	}
	membership, organization, err := model.GetActiveOrganizationMembership(callerId)
	if err == nil && membership.Role == model.OrganizationRoleAdmin && organization.Status == model.OrganizationStatusActive {
		return UsageViewOrganization
	}
	return UsageViewSelf
}

// platformUsageScope resolves a platform report. An organization that does not exist is an
// error, so a typo cannot silently turn into an empty report.
func platformUsageScope(view UsageView, request UsageScopeRequest) (ResolvedUsageScope, error) {
	scope := ResolvedUsageScope{View: view, Scope: model.UsageScope{OrganizationId: request.OrganizationId, UserId: request.UserId}}
	if request.OrganizationId == 0 {
		return scope, nil
	}
	organization, err := model.GetOrganizationById(request.OrganizationId)
	if errors.Is(err, model.ErrOrganizationNotFound) {
		return ResolvedUsageScope{}, ErrUsageOrganizationNotFound
	}
	if err != nil {
		return ResolvedUsageScope{}, err
	}
	scope.Organization = organization
	return scope, nil
}

// UsageRankingFor returns what a ranking groups by for the resolved scope. Platform reports
// rank organizations across the platform and users inside one organization. The organization
// view ranks its members. A personal view has no ranking.
func UsageRankingFor(resolved ResolvedUsageScope) (model.UsageRankBy, bool) {
	switch resolved.View {
	case UsageViewPlatform:
		if resolved.Scope.OrganizationId == 0 {
			return model.UsageRankByOrganization, true
		}
		return model.UsageRankByUser, true
	case UsageViewOrganization:
		return model.UsageRankByUser, true
	default:
		return "", false
	}
}
