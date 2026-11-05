package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorAttributionIsInferredFromCodeAndType(t *testing.T) {
	tests := []struct {
		name       string
		errorType  ErrorType
		errorCode  ErrorCode
		statusCode int
		source     ErrorSource
		stage      ErrorStage
	}{
		{"insufficient user quota is ours, billing", ErrorTypeNewAPIError, ErrorCodeInsufficientUserQuota, http.StatusForbidden, ErrorSourceGateway, ErrorStageBilling},
		{"no available channel is ours, routing", ErrorTypeNewAPIError, ErrorCodeModelNotFound, http.StatusServiceUnavailable, ErrorSourceGateway, ErrorStageRouting},
		{"channel configuration is ours, routing", ErrorTypeNewAPIError, ErrorCode("channel:invalid_key"), http.StatusInternalServerError, ErrorSourceGateway, ErrorStageRouting},
		{"invalid request is the client's", ErrorTypeNewAPIError, ErrorCodeInvalidRequest, http.StatusBadRequest, ErrorSourceClient, ErrorStageRequestBuild},
		{"connection failure is network", ErrorTypeNewAPIError, ErrorCodeDoRequestFailed, http.StatusInternalServerError, ErrorSourceNetwork, ErrorStageUpstreamRequest},
		{"upstream status error is upstream", ErrorTypeNewAPIError, ErrorCodeBadResponseStatusCode, http.StatusBadGateway, ErrorSourceUpstream, ErrorStageUpstreamResponse},
		{"upstream error body is upstream even with our code", ErrorTypeOpenAIError, ErrorCodeInvalidRequest, http.StatusBadRequest, ErrorSourceUpstream, ErrorStageUpstreamResponse},
		{"claude error body is upstream", ErrorTypeClaudeError, ErrorCode("rate_limit"), http.StatusTooManyRequests, ErrorSourceUpstream, ErrorStageUpstreamResponse},
		{"unknown code with 4xx is the client's", ErrorTypeNewAPIError, ErrorCode("something_new"), http.StatusUnprocessableEntity, ErrorSourceClient, ErrorStageUnknown},
		{"unknown code with 5xx is ours", ErrorTypeNewAPIError, ErrorCode("something_new"), http.StatusInternalServerError, ErrorSourceGateway, ErrorStageUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &NewAPIError{errorType: test.errorType, errorCode: test.errorCode, StatusCode: test.statusCode}
			assert.Equal(t, test.source, err.GetErrorSource())
			assert.Equal(t, test.stage, err.GetErrorStage())
		})
	}
}

func TestExplicitErrorAttributionOverridesInference(t *testing.T) {
	err := NewError(errors.New("upstream dropped the connection"), ErrorCodeDoRequestFailed,
		ErrOptionWithErrorAttribution(ErrorSourceNetwork, ErrorStageStream))
	require.NotNil(t, err)

	assert.Equal(t, ErrorSourceNetwork, err.GetErrorSource())
	assert.Equal(t, ErrorStageStream, err.GetErrorStage())
}

func TestNilErrorAttributionIsEmpty(t *testing.T) {
	var err *NewAPIError
	assert.Empty(t, err.GetErrorSource())
	assert.Empty(t, err.GetErrorStage())
}
