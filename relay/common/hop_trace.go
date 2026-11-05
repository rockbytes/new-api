package common

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// Fault sides name the link where a failed request broke. They are written to admin logs only.
const (
	FaultSideClientLink      = "client_link"      // between the user and our gateway
	FaultSideUpstreamLink    = "upstream_link"    // between our gateway and the upstream
	FaultSideUpstreamService = "upstream_service" // the upstream answered, with an error
	FaultSideClientRequest   = "client_request"   // the request itself was invalid
	FaultSideGateway         = "gateway"          // our own processing, not a link
	FaultSideUnknown         = "unknown"
)

const hopUnsetMs = -1

// HopTrace records, per attempt, when the events on the two links happened. Times are
// milliseconds since the trace was created. A nil *HopTrace is valid and records nothing.
type HopTrace struct {
	mu                sync.Mutex
	startedAt         time.Time
	upstreamHost      string
	upstreamStatus    int
	upstreamHeadersMs int64
	upstreamDoneMs    int64
	upstreamErrorMs   int64
	upstreamErrorKind string
	clientGoneMs      int64
	clientWriteErrMs  int64
}

// HopSnapshot is an immutable copy of a HopTrace.
type HopSnapshot struct {
	UpstreamHost      string
	UpstreamStatus    int
	UpstreamHeadersMs int64
	UpstreamDoneMs    int64
	UpstreamErrorMs   int64
	UpstreamErrorKind string
	ClientGoneMs      int64
	ClientWriteErrMs  int64
}

func NewHopTrace() *HopTrace {
	return &HopTrace{
		startedAt:         time.Now(),
		upstreamHeadersMs: hopUnsetMs,
		upstreamDoneMs:    hopUnsetMs,
		upstreamErrorMs:   hopUnsetMs,
		clientGoneMs:      hopUnsetMs,
		clientWriteErrMs:  hopUnsetMs,
	}
}

func (t *HopTrace) since(at time.Time) int64 {
	return at.Sub(t.startedAt).Milliseconds()
}

// MarkUpstreamHeaders records that the upstream answered with a response header.
func (t *HopTrace) MarkUpstreamHeaders(host string, status int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.upstreamHeadersMs == hopUnsetMs {
		t.upstreamHeadersMs = t.since(time.Now())
	}
	t.upstreamHost = host
	t.upstreamStatus = status
}

// MarkUpstreamDone records that the upstream finished sending its response.
func (t *HopTrace) MarkUpstreamDone(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.upstreamDoneMs == hopUnsetMs {
		t.upstreamDoneMs = t.since(at)
	}
}

// MarkUpstreamError records that the link to the upstream failed. kind is one of
// connect, header_timeout, idle_timeout or reset.
func (t *HopTrace) MarkUpstreamError(at time.Time, kind string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.upstreamErrorMs == hopUnsetMs {
		t.upstreamErrorMs = t.since(at)
		t.upstreamErrorKind = kind
	}
}

// MarkClientDisconnect records that the user went away before the response finished.
func (t *HopTrace) MarkClientDisconnect(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.clientGoneMs == hopUnsetMs {
		t.clientGoneMs = t.since(at)
	}
}

// MarkClientWriteError records that writing to the user failed.
func (t *HopTrace) MarkClientWriteError(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.clientWriteErrMs == hopUnsetMs {
		t.clientWriteErrMs = t.since(at)
	}
}

// MarkUpstreamDoError classifies an error returned while sending the upstream request. A
// cancellation means the user went away; a timeout is a response header timeout; a failed dial
// or TLS handshake is a connection failure; anything else is a broken connection.
func (t *HopTrace) MarkUpstreamDoError(at time.Time, err error) {
	if t == nil || err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		t.MarkClientDisconnect(at)
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.MarkUpstreamError(at, "header_timeout")
		return
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (opErr.Op == "dial" || opErr.Op == "tls") {
		t.MarkUpstreamError(at, "connect")
		return
	}
	t.MarkUpstreamError(at, "reset")
}

// MarkStreamEnd maps how a stream ended to link events. A client-side cancellation is a user
// disconnect; a failed write to the user is a client write error; upstream timeouts and read
// failures are upstream link errors; a finished upstream is recorded as done.
func (t *HopTrace) MarkStreamEnd(at time.Time, status *StreamStatus) {
	if t == nil || status == nil {
		return
	}
	switch status.EndReason {
	case StreamEndReasonClientGone:
		if errors.Is(status.EndError, context.Canceled) || errors.Is(status.EndError, context.DeadlineExceeded) {
			t.MarkClientDisconnect(at)
		} else {
			t.MarkClientWriteError(at)
		}
	case StreamEndReasonPingFail:
		t.MarkClientWriteError(at)
	case StreamEndReasonTimeout:
		t.MarkUpstreamError(at, "idle_timeout")
	case StreamEndReasonScannerErr:
		t.MarkUpstreamError(at, "reset")
	case StreamEndReasonDone, StreamEndReasonEOF, StreamEndReasonHandlerStop:
		t.MarkUpstreamDone(at)
	}
}

// Snapshot returns a copy of the recorded events. It is safe on a nil trace.
func (t *HopTrace) Snapshot() HopSnapshot {
	if t == nil {
		return HopSnapshot{UpstreamHeadersMs: hopUnsetMs, UpstreamDoneMs: hopUnsetMs, UpstreamErrorMs: hopUnsetMs,
			ClientGoneMs: hopUnsetMs, ClientWriteErrMs: hopUnsetMs}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return HopSnapshot{
		UpstreamHost:      t.upstreamHost,
		UpstreamStatus:    t.upstreamStatus,
		UpstreamHeadersMs: t.upstreamHeadersMs,
		UpstreamDoneMs:    t.upstreamDoneMs,
		UpstreamErrorMs:   t.upstreamErrorMs,
		UpstreamErrorKind: t.upstreamErrorKind,
		ClientGoneMs:      t.clientGoneMs,
		ClientWriteErrMs:  t.clientWriteErrMs,
	}
}

// Verdict decides which link failed. source is the error source recorded for the failure
// (gateway, client, upstream or network); it is used when the trace has no link event.
//
// The earliest failing event on either link decides the side, because a later event is
// usually a consequence: when the user leaves, the gateway cancels the upstream request, and
// that cancellation is not the cause.
func (s HopSnapshot) Verdict(source string) (side string, reason string) {
	userFailMs := int64(hopUnsetMs)
	userReason := ""
	if s.ClientGoneMs != hopUnsetMs && (s.UpstreamDoneMs == hopUnsetMs || s.ClientGoneMs < s.UpstreamDoneMs) {
		userFailMs = s.ClientGoneMs
		userReason = "client_disconnected"
	}
	if s.ClientWriteErrMs != hopUnsetMs && (userFailMs == hopUnsetMs || s.ClientWriteErrMs < userFailMs) {
		userFailMs = s.ClientWriteErrMs
		userReason = "client_write_failed"
	}

	upstreamFailMs := s.UpstreamErrorMs
	if userFailMs != hopUnsetMs && (upstreamFailMs == hopUnsetMs || userFailMs <= upstreamFailMs) {
		return FaultSideClientLink, userReason
	}
	if upstreamFailMs != hopUnsetMs {
		return FaultSideUpstreamLink, "upstream_" + s.UpstreamErrorKind
	}
	if s.UpstreamStatus >= 400 {
		return FaultSideUpstreamService, "upstream_http_status"
	}

	switch source {
	case "client":
		return FaultSideClientRequest, "invalid_request"
	case "network":
		return FaultSideUpstreamLink, "upstream_network_error"
	case "upstream":
		return FaultSideUpstreamService, "upstream_error_body"
	case "gateway":
		return FaultSideGateway, "gateway_internal"
	}
	return FaultSideUnknown, ""
}

// Map returns the recorded events as a log-ready map, keeping only the events that happened.
func (s HopSnapshot) Map() map[string]any {
	result := map[string]any{}
	if s.UpstreamHost != "" {
		result["upstream_host"] = s.UpstreamHost
	}
	if s.UpstreamStatus != 0 {
		result["upstream_status"] = s.UpstreamStatus
	}
	addMs := func(key string, value int64) {
		if value != hopUnsetMs {
			result[key] = value
		}
	}
	addMs("upstream_headers_ms", s.UpstreamHeadersMs)
	addMs("upstream_done_ms", s.UpstreamDoneMs)
	addMs("upstream_error_ms", s.UpstreamErrorMs)
	addMs("client_gone_ms", s.ClientGoneMs)
	addMs("client_write_error_ms", s.ClientWriteErrMs)
	if s.UpstreamErrorKind != "" {
		result["upstream_error_kind"] = s.UpstreamErrorKind
	}
	return result
}
