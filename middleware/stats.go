package middleware

import (
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// inFlightStats 记录进程内正在处理的 HTTP 请求、已升级的 WebSocket 连接与排空状态，供滚动升级使用。
type inFlightStats struct {
	requests atomic.Int64
	draining atomic.Bool

	// socketsMu serializes draining transitions with registrations, so a
	// connection is notified exactly once whether it registers before or after
	// StartDraining.
	socketsMu sync.Mutex
	sockets   map[*websocket.Conn]func()
}

var globalStats = &inFlightStats{sockets: make(map[*websocket.Conn]func())}

// StatsMiddleware 统计在途 HTTP 请求。WebSocket 升级请求在连接存续期间计为在途。
func StatsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		globalStats.requests.Add(1)
		defer globalStats.requests.Add(-1)

		c.Next()
	}
}

// TrackWebSocket 登记一个已升级的 WebSocket 连接。onDrain 在排空开始后被异步调用一次，
// 由连接所属的处理器决定如何收尾。返回的函数在连接结束时调用，可重复调用。
func TrackWebSocket(conn *websocket.Conn, onDrain func()) (release func()) {
	globalStats.socketsMu.Lock()
	globalStats.sockets[conn] = onDrain
	draining := globalStats.draining.Load()
	globalStats.socketsMu.Unlock()

	if draining {
		go onDrain()
	}

	return func() {
		globalStats.socketsMu.Lock()
		delete(globalStats.sockets, conn)
		globalStats.socketsMu.Unlock()
	}
}

// ActiveWebSocketCount 返回当前已登记的 WebSocket 连接数。
func ActiveWebSocketCount() int {
	globalStats.socketsMu.Lock()
	defer globalStats.socketsMu.Unlock()
	return len(globalStats.sockets)
}

// StartDraining 进入排空状态：此后的 WebSocket 升级会被拒绝，已登记连接的 onDrain 异步触发。重复调用无副作用。
func StartDraining() {
	globalStats.socketsMu.Lock()
	if globalStats.draining.Load() {
		globalStats.socketsMu.Unlock()
		return
	}
	globalStats.draining.Store(true)
	callbacks := slices.Collect(maps.Values(globalStats.sockets))
	globalStats.socketsMu.Unlock()

	for _, onDrain := range callbacks {
		go onDrain()
	}
}

// StopDraining 撤销排空状态。已经关闭的连接不会恢复，仍在宽限期内的 Realtime 连接会因此保持开放。
func StopDraining() {
	globalStats.socketsMu.Lock()
	defer globalStats.socketsMu.Unlock()
	globalStats.draining.Store(false)
}

// IsDraining 报告进程是否处于排空状态。
func IsDraining() bool {
	return globalStats.draining.Load()
}

// CloseWebSocketGoingAway 以 1001 Going Away 结束连接。WriteControl 可与其他方法并发调用，无需额外加锁。
func CloseWebSocketGoingAway(conn *websocket.Conn) {
	closeMessage := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server draining")
	_ = conn.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(time.Second))
	_ = conn.Close()
}

// CloseWebSocketsGoingAway 对所有已登记的连接发送 1001，供关闭截止时间已到时使用。
// 每个连接的收尾由各自的处理器异步完成，调用方需要再等待连接释放。返回被通知的连接数。
func CloseWebSocketsGoingAway() int {
	globalStats.socketsMu.Lock()
	conns := slices.Collect(maps.Keys(globalStats.sockets))
	globalStats.socketsMu.Unlock()

	for _, conn := range conns {
		go CloseWebSocketGoingAway(conn)
	}
	return len(conns)
}

// StatsInfo 统计信息结构
type StatsInfo struct {
	ActiveConnections int64 `json:"active_connections"`
}

// GetStats 获取统计信息
func GetStats() StatsInfo {
	return StatsInfo{
		ActiveConnections: globalStats.requests.Load(),
	}
}
