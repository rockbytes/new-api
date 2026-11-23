package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsMiddlewareCountsRequestUntilHandlerReturns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	entered := make(chan struct{})
	release := make(chan struct{})
	engine := gin.New()
	engine.Use(StatsMiddleware())
	engine.GET("/api/slow", func(c *gin.Context) {
		close(entered)
		<-release
		c.Status(http.StatusOK)
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/slow", nil))
	}()

	<-entered
	assert.Equal(t, int64(1), GetStats().ActiveConnections)
	close(release)
	<-done
	assert.Equal(t, int64(0), GetStats().ActiveConnections)
}

func TestTrackWebSocketCountsUntilReleased(t *testing.T) {
	first := &websocket.Conn{}
	second := &websocket.Conn{}

	releaseFirst := TrackWebSocket(first, func() {})
	releaseSecond := TrackWebSocket(second, func() {})
	require.Equal(t, 2, ActiveWebSocketCount())

	releaseFirst()
	releaseFirst()
	assert.Equal(t, 1, ActiveWebSocketCount(), "releasing the same connection twice must not remove another connection")

	releaseSecond()
	assert.Equal(t, 0, ActiveWebSocketCount())
}

// The deadline path closes every tracked connection with 1001, so the peer sees a
// going-away close rather than a reset.
func TestCloseWebSocketsGoingAwayEndsTrackedConnections(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	tracked := <-accepted
	release := TrackWebSocket(tracked, func() {})
	defer release()

	require.Equal(t, 1, CloseWebSocketsGoingAway())
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = client.ReadMessage()
	var closeErr *websocket.CloseError
	require.ErrorAs(t, err, &closeErr)
	assert.Equal(t, websocket.CloseGoingAway, closeErr.Code)
}

// Each tracked socket is notified once when draining starts, whether it was
// registered before or after StartDraining, and a second StartDraining does not
// notify again.
func TestStartDrainingNotifiesEachSocketOnce(t *testing.T) {
	t.Cleanup(StopDraining)
	before := &websocket.Conn{}
	after := &websocket.Conn{}
	notified := make(chan *websocket.Conn, 4)
	releaseBefore := TrackWebSocket(before, func() { notified <- before })
	defer releaseBefore()
	require.False(t, IsDraining())

	StartDraining()
	require.True(t, IsDraining())
	releaseAfter := TrackWebSocket(after, func() { notified <- after })
	defer releaseAfter()

	seen := map[*websocket.Conn]int{}
	for range 2 {
		select {
		case conn := <-notified:
			seen[conn]++
		case <-time.After(time.Second):
			require.FailNow(t, "tracked socket was not notified after draining started")
		}
	}
	assert.Equal(t, map[*websocket.Conn]int{before: 1, after: 1}, seen)

	StartDraining()
	select {
	case conn := <-notified:
		assert.Failf(t, "repeated StartDraining notified again", "%p", conn)
	case <-time.After(50 * time.Millisecond):
	}

	StopDraining()
	assert.False(t, IsDraining())
}
