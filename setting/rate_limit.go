package setting

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const MaxModelRateLimitMinutes = 24 * 60

// Legacy group settings accept large counts. Retain that range for existing deployments.
const maxModelRequestRateLimitCount int64 = math.MaxInt64 / (MaxModelRateLimitMinutes * 60)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

type ModelRequestRateLimitConfig struct {
	Enabled         bool
	DurationMinutes int
	Total           int
	Success         int
	// Published group maps are immutable and replaced on update.
	Groups map[string][2]int
}

func CurrentModelRequestRateLimit() ModelRequestRateLimitConfig {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()
	return ModelRequestRateLimitConfig{
		Enabled: ModelRequestRateLimitEnabled, DurationMinutes: ModelRequestRateLimitDurationMinutes,
		Total: ModelRequestRateLimitCount, Success: ModelRequestRateLimitSuccessCount, Groups: ModelRequestRateLimitGroup,
	}
}

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog(common.LogText("error marshalling model ratio: %s", err.Error()))
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	groups, err := parseModelRequestRateLimitGroups(jsonStr)
	if err != nil {
		return err
	}
	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()
	ModelRequestRateLimitGroup = groups
	return nil
}

func ValidateModelRequestRateLimitNumber(key string, value string) error {
	limit, err := strconv.ParseInt(value, 10, 64)
	upper := maxModelRequestRateLimitCount
	if key == "ModelRequestRateLimitDurationMinutes" {
		upper = MaxModelRateLimitMinutes
	}
	if err != nil || limit < 0 || limit > upper {
		return fmt.Errorf("%s must be an integer between 0 and %d", key, upper)
	}
	return nil
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	_, err := parseModelRequestRateLimitGroups(jsonStr)
	return err
}

func parseModelRequestRateLimitGroups(jsonStr string) (map[string][2]int, error) {
	var rawGroups map[string][]*int
	if err := common.Unmarshal([]byte(jsonStr), &rawGroups); err != nil {
		return nil, err
	}
	if rawGroups == nil {
		return nil, fmt.Errorf("group rate limits must be a JSON object")
	}
	groups := make(map[string][2]int, len(rawGroups))
	for group, values := range rawGroups {
		if group == "" || len(values) != 2 || values[0] == nil || values[1] == nil {
			return nil, fmt.Errorf("group %q must configure both total and successful request limits", group)
		}
		total, success := *values[0], *values[1]
		if total < 0 || success < 0 || int64(total) > maxModelRequestRateLimitCount || int64(success) > maxModelRequestRateLimitCount {
			return nil, fmt.Errorf("group %q must have request limits between 0 and %d", group, maxModelRequestRateLimitCount)
		}
		groups[group] = [2]int{total, success}
	}
	return groups, nil
}
