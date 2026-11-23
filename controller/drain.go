package controller

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
)

// drainStatus 字段顺序与升级脚本中的 grep 模式一致，请勿调整。
type drainStatus struct {
	State            string `json:"state"`
	ActiveRequests   int64  `json:"active_requests"`
	ActiveWebSockets int    `json:"active_websockets"`
}

func newDrainHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /drain/status", handleDrainStatus)
	mux.HandleFunc("POST /drain", handleDrainStart)
	mux.HandleFunc("DELETE /drain", handleDrainStop)
	return mux
}

func handleDrainStatus(w http.ResponseWriter, _ *http.Request) {
	writeDrainStatus(w, http.StatusOK)
}

// handleDrainStart returns before any connection closes; the upgrade script polls status.
func handleDrainStart(w http.ResponseWriter, _ *http.Request) {
	middleware.StartDraining()
	writeDrainStatus(w, http.StatusAccepted)
}

func handleDrainStop(w http.ResponseWriter, _ *http.Request) {
	middleware.StopDraining()
	writeDrainStatus(w, http.StatusOK)
}

func writeDrainStatus(w http.ResponseWriter, status int) {
	state := "running"
	if middleware.IsDraining() {
		state = "draining"
	}
	body, err := common.Marshal(drainStatus{
		State:            state,
		ActiveRequests:   middleware.GetStats().ActiveConnections,
		ActiveWebSockets: middleware.ActiveWebSocketCount(),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ServeDrainSocket serves the drain endpoints on a unix socket that only the
// owner and group can connect to. It returns once the socket is listening.
// Failure is returned to the caller, which logs it: drain is operational
// tooling and must not stop the gateway from starting.
func ServeDrainSocket(path string) (*http.Server, error) {
	// A previous process killed by a signal leaves its socket file behind, and
	// bind would fail with "address already in use".
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = listener.Close()
		return nil, err
	}
	server := &http.Server{Handler: newDrainHandler()}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.SysError(common.LogText("drain socket stopped: %v", err))
		}
	}()
	return server, nil
}
