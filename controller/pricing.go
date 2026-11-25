package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type catalogPricing struct {
	model.Pricing
	SupportedEndpointsByGroup map[string][]constant.EndpointType            `json:"supported_endpoints_by_group"`
	EndpointOverrides         map[constant.EndpointType]common.EndpointInfo `json:"endpoint_overrides,omitempty"`
}

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroups map[string]float64, groupEndpoints map[string]map[string][]constant.EndpointType, endpointOverrides map[string]map[constant.EndpointType]common.EndpointInfo) []catalogPricing {
	if len(usableGroups) == 0 {
		return []catalogPricing{}
	}

	filtered := make([]catalogPricing, 0, len(pricing))
	for _, item := range pricing {
		groups := make([]string, 0, len(item.EnableGroup))
		endpointsByGroup := make(map[string][]constant.EndpointType)
		availableEndpoints := make(map[constant.EndpointType]struct{})
		for _, group := range item.EnableGroup {
			if _, ok := usableGroups[group]; !ok {
				continue
			}
			groups = append(groups, group)
			endpoints := groupEndpoints[item.ModelName][group]
			endpointsByGroup[group] = endpoints
			for _, endpoint := range endpoints {
				availableEndpoints[endpoint] = struct{}{}
			}
		}
		if len(groups) == 0 {
			continue
		}

		// The cached record and its maps/slices are shared across viewers.
		slices.Sort(groups)
		item.EnableGroup = groups
		endpoints := make([]constant.EndpointType, 0, len(availableEndpoints))
		overrides := make(map[constant.EndpointType]common.EndpointInfo)
		for _, endpoint := range item.SupportedEndpointTypes {
			if _, ok := availableEndpoints[endpoint]; !ok {
				continue
			}
			endpoints = append(endpoints, endpoint)
			if info, ok := endpointOverrides[item.ModelName][endpoint]; ok {
				overrides[endpoint] = info
			}
		}
		item.SupportedEndpointTypes = endpoints
		filtered = append(filtered, catalogPricing{
			Pricing: item, SupportedEndpointsByGroup: endpointsByGroup, EndpointOverrides: overrides,
		})
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing, vendors, groupEndpoints, endpointOverrides := model.GetPricingCatalog()
	userId, exists := c.Get("id")
	groupRatio := ratio_setting.GetGroupRatioCopy()
	var group string
	role := common.RoleCommonUser
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			role = user.Role
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup := service.GetUserUsableGroups(group, role)
	for group := range groupRatio {
		if _, ok := usableGroup[group]; !ok || group == "" || group == "auto" || group == "all" {
			delete(groupRatio, group)
		}
	}
	for name := range usableGroup {
		if name == "auto" {
			continue
		}
		if _, ok := groupRatio[name]; !ok {
			delete(usableGroup, name)
		}
	}
	catalog := filterPricingByUsableGroups(pricing, groupRatio, groupEndpoints, endpointOverrides)
	// A shared legacy map is safe only when all visible models resolve the
	// endpoint to the same path. Per-model overrides remain authoritative for
	// new clients when those paths differ.
	supportedEndpoints := make(map[string]common.EndpointInfo)
	defaultEndpoints := make(map[string]common.EndpointInfo)
	conflicting := make(map[string]bool)
	for _, item := range catalog {
		for _, endpoint := range item.SupportedEndpointTypes {
			key := string(endpoint)
			defaultInfo, hasDefault := common.GetDefaultEndpointInfo(endpoint)
			if hasDefault {
				defaultEndpoints[key] = defaultInfo
			}
			info, hasOverride := item.EndpointOverrides[endpoint]
			if !hasOverride {
				info = defaultInfo
			}
			if info.Path == "" {
				conflicting[key] = true
				continue
			}
			if previous, exists := supportedEndpoints[key]; exists && previous != info {
				conflicting[key] = true
			} else if !exists {
				supportedEndpoints[key] = info
			}
		}
	}
	for endpoint := range conflicting {
		delete(supportedEndpoints, endpoint)
	}

	c.JSON(200, gin.H{
		"catalog_capabilities_version": 1,
		"success":                      true,
		"data":                         catalog,
		"vendors":                      vendors,
		"group_ratio":                  groupRatio,
		"usable_group":                 usableGroup,
		"supported_endpoint":           supportedEndpoints,
		"default_endpoints":            defaultEndpoints,
		"auto_groups":                  service.GetUserAutoGroup(group, role),
		"pricing_version":              "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccessT(c, "Model ratios reset", nil)
}
