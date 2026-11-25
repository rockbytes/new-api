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

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroups map[string]float64) []model.Pricing {
	if len(usableGroups) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		groups := make([]string, 0, len(item.EnableGroup))
		endpointsByGroup := make(map[string][]constant.EndpointType)
		availableEndpoints := make(map[constant.EndpointType]struct{})
		for _, group := range item.EnableGroup {
			if _, ok := usableGroups[group]; !ok {
				continue
			}
			groups = append(groups, group)
			endpoints := item.SupportedEndpointsByGroup[group]
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
		item.SupportedEndpointsByGroup = endpointsByGroup
		endpoints := make([]constant.EndpointType, 0, len(availableEndpoints))
		overrides := make(map[constant.EndpointType]common.EndpointInfo)
		for _, endpoint := range item.SupportedEndpointTypes {
			if _, ok := availableEndpoints[endpoint]; !ok {
				continue
			}
			endpoints = append(endpoints, endpoint)
			if info, ok := item.EndpointOverrides[endpoint]; ok {
				overrides[endpoint] = info
			}
		}
		item.SupportedEndpointTypes = endpoints
		item.EndpointOverrides = overrides
		filtered = append(filtered, item)
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
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
	pricing = filterPricingByUsableGroups(pricing, groupRatio)
	supportedEndpoints := make(map[string]common.EndpointInfo)
	for _, item := range pricing {
		for _, endpoint := range item.SupportedEndpointTypes {
			if info, ok := common.GetDefaultEndpointInfo(endpoint); ok {
				supportedEndpoints[string(endpoint)] = info
			}
		}
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": supportedEndpoints,
		"auto_groups":        service.GetUserAutoGroup(group, role),
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
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
