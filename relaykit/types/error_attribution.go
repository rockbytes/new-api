package types

import "strings"

// ErrorSource is the party responsible for an error, as recorded in error logs.
type ErrorSource string

const (
	ErrorSourceGateway  ErrorSource = "gateway"  // our gateway: auth, quota, routing, channel config, our own conversion
	ErrorSourceClient   ErrorSource = "client"   // the caller's request: invalid parameters, unknown model, unreadable body
	ErrorSourceUpstream ErrorSource = "upstream" // an upstream provider or upstream gateway answered with an error
	ErrorSourceNetwork  ErrorSource = "network"  // connecting to, or waiting for, the upstream failed; ownership is not decided
)

// ErrorStage is the processing step where an error happened.
type ErrorStage string

const (
	ErrorStageAuth             ErrorStage = "auth"
	ErrorStageRouting          ErrorStage = "routing"
	ErrorStageBilling          ErrorStage = "billing"
	ErrorStageRequestBuild     ErrorStage = "request_build"
	ErrorStageUpstreamRequest  ErrorStage = "upstream_request"
	ErrorStageUpstreamResponse ErrorStage = "upstream_response"
	ErrorStageStream           ErrorStage = "stream"
	ErrorStageResponseBuild    ErrorStage = "response_build"
	ErrorStageUnknown          ErrorStage = "unknown"
)

// errorCodeAttribution maps error codes that always have the same owner and stage.
var errorCodeAttribution = map[ErrorCode]struct {
	source ErrorSource
	stage  ErrorStage
}{
	ErrorCodeInsufficientUserQuota:         {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeInsufficientOrganizationQuota: {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeInsufficientMemberQuota:       {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodePreConsumeTokenQuotaFailed:    {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeOrganizationMemberInactive:    {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeOrganizationInactive:          {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeModelPriceError:               {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeQueryDataError:                {ErrorSourceGateway, ErrorStageBilling},
	ErrorCodeUpdateDataError:               {ErrorSourceGateway, ErrorStageBilling},

	ErrorCodeAccessDenied:       {ErrorSourceGateway, ErrorStageAuth},
	ErrorCodeModelNotFound:      {ErrorSourceGateway, ErrorStageRouting},
	ErrorCodeGetChannelFailed:   {ErrorSourceGateway, ErrorStageRouting},
	ErrorCodeGenRelayInfoFailed: {ErrorSourceGateway, ErrorStageRouting},
	ErrorCodeInvalidApiType:     {ErrorSourceGateway, ErrorStageRouting},

	ErrorCodeInvalidRequest:         {ErrorSourceClient, ErrorStageRequestBuild},
	ErrorCodeBadRequestBody:         {ErrorSourceClient, ErrorStageRequestBuild},
	ErrorCodeReadRequestBodyFailed:  {ErrorSourceClient, ErrorStageRequestBuild},
	ErrorCodeSensitiveWordsDetected: {ErrorSourceClient, ErrorStageRequestBuild},
	ErrorCodePromptBlocked:          {ErrorSourceClient, ErrorStageRequestBuild},
	ErrorCodeViolationFeeGrokCSAM:   {ErrorSourceClient, ErrorStageRequestBuild},

	ErrorCodeConvertRequestFailed: {ErrorSourceGateway, ErrorStageRequestBuild},
	ErrorCodeJsonMarshalFailed:    {ErrorSourceGateway, ErrorStageRequestBuild},
	ErrorCodeCountTokenFailed:     {ErrorSourceGateway, ErrorStageRequestBuild},

	ErrorCodeDoRequestFailed: {ErrorSourceNetwork, ErrorStageUpstreamRequest},

	ErrorCodeBadResponseStatusCode:  {ErrorSourceUpstream, ErrorStageUpstreamResponse},
	ErrorCodeBadResponse:            {ErrorSourceUpstream, ErrorStageUpstreamResponse},
	ErrorCodeBadResponseBody:        {ErrorSourceUpstream, ErrorStageUpstreamResponse},
	ErrorCodeEmptyResponse:          {ErrorSourceUpstream, ErrorStageUpstreamResponse},
	ErrorCodeReadResponseBodyFailed: {ErrorSourceUpstream, ErrorStageUpstreamResponse},
	ErrorCodeAwsInvokeError:         {ErrorSourceUpstream, ErrorStageUpstreamResponse},
}

// upstreamErrorTypes are error types that carry an error body returned by an upstream provider.
var upstreamErrorTypes = map[ErrorType]struct{}{
	ErrorTypeOpenAIError:     {},
	ErrorTypeClaudeError:     {},
	ErrorTypeGeminiError:     {},
	ErrorTypeMidjourneyError: {},
	ErrorTypeRerankError:     {},
	ErrorTypeUpstreamError:   {},
}

// inferErrorAttribution decides the source and stage of an error that did not set them
// explicitly. Error types that come from upstream bodies are upstream errors. Otherwise the
// error code decides; unknown codes fall back to the HTTP status: 4xx is the caller's
// request, anything else is ours.
func inferErrorAttribution(errorType ErrorType, errorCode ErrorCode, statusCode int) (ErrorSource, ErrorStage) {
	if _, ok := upstreamErrorTypes[errorType]; ok {
		return ErrorSourceUpstream, ErrorStageUpstreamResponse
	}
	if attribution, ok := errorCodeAttribution[errorCode]; ok {
		return attribution.source, attribution.stage
	}
	if isChannelErrorCode(errorCode) {
		return ErrorSourceGateway, ErrorStageRouting
	}
	if statusCode >= 400 && statusCode < 500 {
		return ErrorSourceClient, ErrorStageUnknown
	}
	return ErrorSourceGateway, ErrorStageUnknown
}

func isChannelErrorCode(errorCode ErrorCode) bool {
	_, ok := strings.CutPrefix(string(errorCode), "channel:")
	return ok
}
