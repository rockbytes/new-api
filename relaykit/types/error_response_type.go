package types

import "net/http"

// OpenAIErrorTypeForStatus is the standard error type sent to OpenAI-compatible clients for a
// gateway error with the given HTTP status. The specific reason is carried by the error code.
func OpenAIErrorTypeForStatus(statusCode int) string {
	switch {
	case statusCode == http.StatusUnauthorized:
		return "authentication_error"
	case statusCode == http.StatusForbidden:
		return "permission_error"
	case statusCode == http.StatusTooManyRequests:
		return "rate_limit_error"
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}

// ClaudeErrorTypeForStatus is the standard error type sent to Claude-compatible clients for a
// gateway error with the given HTTP status.
func ClaudeErrorTypeForStatus(statusCode int) string {
	switch {
	case statusCode == http.StatusUnauthorized:
		return "authentication_error"
	case statusCode == http.StatusForbidden:
		return "permission_error"
	case statusCode == http.StatusNotFound:
		return "not_found_error"
	case statusCode == http.StatusTooManyRequests:
		return "rate_limit_error"
	case statusCode == 529:
		return "overloaded_error"
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return "invalid_request_error"
	default:
		return "api_error"
	}
}
