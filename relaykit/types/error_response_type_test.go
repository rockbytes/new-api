package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStandardErrorTypeFollowsTheHTTPStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		openai string
		claude string
	}{
		{http.StatusBadRequest, "invalid_request_error", "invalid_request_error"},
		{http.StatusUnauthorized, "authentication_error", "authentication_error"},
		{http.StatusForbidden, "permission_error", "permission_error"},
		{http.StatusNotFound, "invalid_request_error", "not_found_error"},
		{http.StatusTooManyRequests, "rate_limit_error", "rate_limit_error"},
		{http.StatusInternalServerError, "server_error", "api_error"},
		{http.StatusServiceUnavailable, "server_error", "api_error"},
		{529, "server_error", "overloaded_error"},
	}
	for _, test := range tests {
		assert.Equal(t, test.openai, OpenAIErrorTypeForStatus(test.status), "openai status %d", test.status)
		assert.Equal(t, test.claude, ClaudeErrorTypeForStatus(test.status), "claude status %d", test.status)
	}
}

func TestGatewayErrorsSendTheStandardTypeWithTheSpecificCode(t *testing.T) {
	t.Parallel()
	err := NewErrorWithStatusCode(errors.New("no channel"), ErrorCodeModelNotFound, http.StatusServiceUnavailable)

	openAI := err.ToOpenAIError()
	assert.Equal(t, "server_error", openAI.Type)
	assert.Equal(t, ErrorCode("model_not_found"), openAI.Code)

	claude := err.ToClaudeError()
	assert.Equal(t, "api_error", claude.Type)
}

func TestUpstreamErrorTypesAreNotRewritten(t *testing.T) {
	t.Parallel()
	upstream := WithOpenAIError(OpenAIError{Message: "bad param", Type: "invalid_request_error", Code: "bad_param"}, http.StatusBadRequest)

	assert.Equal(t, "invalid_request_error", upstream.ToOpenAIError().Type)
	assert.Equal(t, string(ErrorTypeOpenAIError), string(upstream.GetErrorType()))
}
