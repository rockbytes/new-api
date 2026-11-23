package controller

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveDrain(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

func decodeDrainStatus(t *testing.T, recorder *httptest.ResponseRecorder) drainStatus {
	t.Helper()
	var status drainStatus
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
	return status
}

// Draining is reported and toggled through the endpoints, and entering it notifies
// tracked WebSocket connections so they can end.
func TestDrainEndpointsReportCountsAndToggleDraining(t *testing.T) {
	t.Cleanup(middleware.StopDraining)
	handler := newDrainHandler()
	notified := make(chan struct{}, 1)
	release := middleware.TrackWebSocket(&websocket.Conn{}, func() { notified <- struct{}{} })
	defer release()

	recorder := serveDrain(handler, http.MethodGet, "/drain/status")
	require.Equal(t, http.StatusOK, recorder.Code)
	status := decodeDrainStatus(t, recorder)
	assert.Equal(t, "running", status.State)
	assert.Equal(t, 1, status.ActiveWebSockets)
	assert.Equal(t, middleware.GetStats().ActiveConnections, status.ActiveRequests)

	recorder = serveDrain(handler, http.MethodPost, "/drain")
	require.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "draining", decodeDrainStatus(t, recorder).State)
	select {
	case <-notified:
	case <-time.After(time.Second):
		require.FailNow(t, "POST /drain did not notify the tracked WebSocket")
	}

	recorder = serveDrain(handler, http.MethodDelete, "/drain")
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "running", decodeDrainStatus(t, recorder).State)
	assert.False(t, middleware.IsDraining())
}

// The socket is replaced even when a previous process left a file behind, and it
// is reachable through the unix socket with owner/group permissions only.
func TestServeDrainSocketReplacesLeftoverFileAndRestrictsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket behaviour is verified on the Linux gateway host")
	}
	path := filepath.Join(t.TempDir(), "drain.sock")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	server, err := ServeDrainSocket(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&fs.ModeSocket)
	assert.Equal(t, fs.FileMode(0o660), info.Mode().Perm())

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
	resp, err := client.Get("http://drain/drain/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// A socket that cannot be bound is returned as an error, which the caller logs.
// Startup does not depend on the drain listener.
func TestServeDrainSocketReturnsBindFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket behaviour is verified on the Linux gateway host")
	}
	_, err := ServeDrainSocket(filepath.Join(t.TempDir(), "missing", "drain.sock"))
	require.Error(t, err)
}
