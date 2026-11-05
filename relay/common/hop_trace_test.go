package common

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

// traceAt returns a fresh trace and a helper that gives the time t milliseconds after it started.
func traceAt() (*HopTrace, func(ms int64) time.Time) {
	trace := NewHopTrace()
	return trace, func(ms int64) time.Time {
		return trace.startedAt.Add(time.Duration(ms) * time.Millisecond)
	}
}

func TestHopVerdictNamesTheLinkWhoseEventHappenedFirst(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		events func(trace *HopTrace, at func(int64) time.Time)
		status int
		source string
		side   string
		reason string
	}{
		{
			name: "user leaves before the upstream fails",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkClientDisconnect(at(100))
				tr.MarkUpstreamError(at(300), "reset")
			},
			side: FaultSideClientLink, reason: "client_disconnected",
		},
		{
			name: "upstream resets and the user disconnect is only the consequence",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkUpstreamError(at(200), "reset")
				tr.MarkClientDisconnect(at(210))
			},
			side: FaultSideUpstreamLink, reason: "upstream_reset",
		},
		{
			name: "upstream refuses the connection",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkUpstreamError(at(5), "connect")
			},
			side: FaultSideUpstreamLink, reason: "upstream_connect",
		},
		{
			name: "upstream sends no response header in time",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkUpstreamError(at(5000), "header_timeout")
			},
			side: FaultSideUpstreamLink, reason: "upstream_header_timeout",
		},
		{
			name: "writing to the user fails first",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkClientWriteError(at(50))
			},
			side: FaultSideClientLink, reason: "client_write_failed",
		},
		{
			name: "the user leaves after the upstream finished, which is not a failure",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkUpstreamDone(at(300))
				tr.MarkClientDisconnect(at(400))
			},
			side: FaultSideUnknown, reason: "",
		},
		{
			name: "the upstream answered with an error status",
			events: func(tr *HopTrace, at func(int64) time.Time) {
				tr.MarkUpstreamHeaders("up.example", 500)
			},
			status: 500,
			side:   FaultSideUpstreamService, reason: "upstream_http_status",
		},
		{
			name:   "no link event: the request was invalid",
			source: "client",
			side:   FaultSideClientRequest, reason: "invalid_request",
		},
		{
			name:   "no link event: our own processing failed",
			source: "gateway",
			side:   FaultSideGateway, reason: "gateway_internal",
		},
		{
			name:   "no link event: the upstream returned an error body",
			source: "upstream",
			side:   FaultSideUpstreamService, reason: "upstream_error_body",
		},
		{
			name:   "no trace: a network error without link detail",
			source: "network",
			side:   FaultSideUpstreamLink, reason: "upstream_network_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			trace, at := traceAt()
			if test.events != nil {
				test.events(trace, at)
			}
			if test.status != 0 && test.events == nil {
				trace.MarkUpstreamHeaders("up.example", test.status)
			}
			side, reason := trace.Snapshot().Verdict(test.source)
			assert.Equal(t, test.side, side)
			assert.Equal(t, test.reason, reason)
		})
	}
}

func TestHopDoErrorIsClassifiedByWhichLinkFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"dial failed", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, "connect"},
		{"tls handshake failed", &net.OpError{Op: "tls", Err: errors.New("bad certificate")}, "connect"},
		{"timed out waiting for headers", timeoutNetErr{}, "header_timeout"},
		{"connection reset", errors.New("connection reset by peer"), "reset"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			trace, at := traceAt()
			trace.MarkUpstreamDoError(at(10), test.err)
			assert.Equal(t, test.want, trace.Snapshot().UpstreamErrorKind)
		})
	}

	t.Run("the caller cancelled is the user's link", func(t *testing.T) {
		t.Parallel()
		trace, at := traceAt()
		trace.MarkUpstreamDoError(at(10), context.Canceled)
		snap := trace.Snapshot()
		assert.Equal(t, int64(10), snap.ClientGoneMs)
		assert.Equal(t, int64(-1), snap.UpstreamErrorMs)
	})
}

func TestHopStreamEndMapsEachEndReasonToItsLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason StreamEndReason
		endErr error
		check  func(t *testing.T, snap HopSnapshot)
	}{
		{"user cancelled", StreamEndReasonClientGone, context.Canceled, func(t *testing.T, s HopSnapshot) {
			assert.NotEqual(t, int64(-1), s.ClientGoneMs)
		}},
		{"writing to the user failed", StreamEndReasonClientGone, errors.New("broken pipe"), func(t *testing.T, s HopSnapshot) {
			assert.NotEqual(t, int64(-1), s.ClientWriteErrMs)
			assert.Equal(t, int64(-1), s.ClientGoneMs)
		}},
		{"upstream went silent", StreamEndReasonTimeout, nil, func(t *testing.T, s HopSnapshot) {
			assert.Equal(t, "idle_timeout", s.UpstreamErrorKind)
		}},
		{"upstream connection broke", StreamEndReasonScannerErr, errors.New("read tcp: reset"), func(t *testing.T, s HopSnapshot) {
			assert.Equal(t, "reset", s.UpstreamErrorKind)
		}},
		{"upstream finished", StreamEndReasonEOF, nil, func(t *testing.T, s HopSnapshot) {
			assert.NotEqual(t, int64(-1), s.UpstreamDoneMs)
			assert.Equal(t, int64(-1), s.UpstreamErrorMs)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			trace, at := traceAt()
			status := NewStreamStatus()
			status.SetEndReason(test.reason, test.endErr)
			trace.MarkStreamEnd(at(100), status)
			test.check(t, trace.Snapshot())
		})
	}
}

func TestHopTraceIsSafeWhenAbsent(t *testing.T) {
	t.Parallel()
	var trace *HopTrace
	trace.MarkUpstreamHeaders("x", 200)
	trace.MarkUpstreamDoError(time.Now(), errors.New("x"))
	trace.MarkStreamEnd(time.Now(), NewStreamStatus())
	side, reason := trace.Snapshot().Verdict("gateway")
	assert.Equal(t, FaultSideGateway, side)
	assert.Equal(t, "gateway_internal", reason)
	assert.Empty(t, trace.Snapshot().Map())
}
