package middleware

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	_, preparedPluginRoute := c.Get(pluginruntime.ContextKeyRouteRequest)
	if !preparedPluginRoute || !RespondTaskPluginError(c, &dto.TaskError{
		Code:       codeStr,
		Message:    message,
		StatusCode: statusCode,
	}) {
		c.JSON(statusCode, gin.H{
			"error": gin.H{
				"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
				"type":    types.OpenAIErrorTypeForStatus(statusCode),
				"code":    codeStr,
			},
		})
	}
	c.Abort()
	logger.LogError(c.Request.Context(), common.LogText("user %d | %s", userId, message))
}

// setRateLimitHeaders reports the limit that refused a request. Its window is spent,
// so remaining is 0. A window length is not a wait time, so no reset is set here.
func setRateLimitHeaders(c *gin.Context, limit int64) {
	c.Header("x-ratelimit-limit", strconv.FormatInt(limit, 10))
	c.Header("x-ratelimit-remaining", "0")
}

// SetChannelRateLimitHeaders reports a refused channel budget. Its reset is exact: the
// time to refill one request, the same hint as Retry-After.
func SetChannelRateLimitHeaders(c *gin.Context, limit int64, resetSeconds int64) {
	setRateLimitHeaders(c, limit)
	c.Header("x-ratelimit-reset", strconv.FormatInt(resetSeconds, 10))
}

// abortWithRateLimitedScope refuses a request with the OpenAI error body and also
// names the rate limit layer that refused it in rate_limited_scope (design D4).
func abortWithRateLimitedScope(c *gin.Context, statusCode int, message string, code types.ErrorCode, scope string) {
	_, preparedPluginRoute := c.Get(pluginruntime.ContextKeyRouteRequest)
	if preparedPluginRoute {
		abortWithOpenAiMessage(c, statusCode, message, code)
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message":            common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":               types.OpenAIErrorTypeForStatus(statusCode),
			"code":               string(code),
			"rate_limited_scope": scope,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), common.LogText("user %d | %s", c.GetInt("id"), message))
}

// invalidRequestStatus is the status of a request whose body could not be
// read or parsed: 413 when the body exceeds MAX_REQUEST_BODY_MB, else 400.
func invalidRequestStatus(err error) int {
	if common.IsRequestBodyTooLargeError(err) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        types.OpenAIErrorTypeForStatus(statusCode),
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
