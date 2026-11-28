package openai

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const realtimeWriteTimeout = 10 * time.Second

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *service.MeteredRealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	client, target := info.ClientWs, info.TargetWs
	clientDone, targetDone := make(chan struct{}), make(chan struct{})
	stop := make(chan struct{})
	var mu sync.Mutex // local usage and the token counter's tools/audio settings
	var localUsage dto.RealtimeUsage
	var metered service.MeteredRealtimeUsage
	var billingErr *types.NewAPIError

	go func() {
		defer close(clientDone)
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("panic in realtime client reader: %v", r))
			}
		}()
		for {
			_, message, err := client.ReadMessage()
			if err != nil {
				return
			}
			var event dto.RealtimeEvent
			if err := common.Unmarshal(message, &event); err != nil {
				logger.LogError(c, fmt.Sprintf("invalid realtime client event: %v", err))
				return
			}

			mu.Lock()
			func() {
				defer mu.Unlock()
				if event.Type == dto.RealtimeEventTypeSessionUpdate && event.Session != nil && event.Session.Tools != nil {
					info.RealtimeTools = event.Session.Tools
				}
				err = countRealtimeUsage(info, event, &localUsage, true)
			}()
			if err != nil {
				logger.LogError(c, err.Error())
				return
			}

			if err := helper.WssString(c, target, string(message)); err != nil {
				return
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	}()

	go func() {
		defer close(targetDone)
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("panic in realtime upstream reader: %v", r))
			}
		}()
		for {
			_, message, err := target.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					select {
					case <-stop:
					default:
						logger.LogError(c, fmt.Sprintf("error reading realtime upstream: %v", err))
					}
				}
				return
			}
			info.SetFirstResponseTime()
			var event dto.RealtimeEvent
			if err := common.Unmarshal(message, &event); err != nil {
				logger.LogError(c, fmt.Sprintf("invalid realtime upstream event: %v", err))
				return
			}

			switch event.Type {
			case dto.RealtimeEventTypeResponseDone:
				candidate := metered.Usage
				mu.Lock()
				err = func() error {
					defer mu.Unlock()
					local := localUsage
					localUsage = dto.RealtimeUsage{}
					if event.Response != nil && event.Response.Usage != nil {
						return addRealtimeUsage(&candidate, *event.Response.Usage)
					}
					err := countRealtimeUsage(info, event, &local, true)
					info.IsFirstRequest = false
					if err != nil {
						return err
					}
					return addRealtimeUsage(&candidate, local)
				}()
				// Pricing and storage must not hold the lock for the peer reader.
				if err != nil {
					logger.LogError(c, err.Error())
					return
				}
				billingErr = metered.Reserve(c, info, info.UpstreamModelName, candidate)
				if billingErr != nil {
					return
				}
			case dto.RealtimeEventTypeSessionUpdated, dto.RealtimeEventTypeSessionCreated:
				if session := event.Session; session != nil {
					mu.Lock()
					info.InputAudioFormat = common.GetStringIfEmpty(session.InputAudioFormat, info.InputAudioFormat)
					info.OutputAudioFormat = common.GetStringIfEmpty(session.OutputAudioFormat, info.OutputAudioFormat)
					mu.Unlock()
				}
			default:
				mu.Lock()
				err = func() error {
					defer mu.Unlock()
					return countRealtimeUsage(info, event, &localUsage, false)
				}()
				if err != nil {
					logger.LogError(c, err.Error())
					return
				}
			}

			select {
			case <-stop:
				return
			default:
			}
			_ = client.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout))
			if err := helper.WssString(c, client, string(message)); err != nil {
				return
			}
		}
	}()

	select {
	case <-clientDone:
	case <-targetDone:
	case <-c.Request.Context().Done():
	}
	// Wake both readers before using their final usage. Keep the client write
	// side open so the controller can report a terminal billing error.
	close(stop)
	_ = target.Close()
	_ = client.UnderlyingConn().SetReadDeadline(time.Now())
	<-clientDone
	<-targetDone
	// Bound the controller's final error write as well; this socket is closing.
	_ = client.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout))

	if billingErr == nil && localUsage.TotalTokens > 0 {
		candidate := metered.Usage
		if err := addRealtimeUsage(&candidate, localUsage); err != nil {
			logger.LogError(c, err.Error())
		} else {
			billingErr = metered.Reserve(c, info, info.UpstreamModelName, candidate)
		}
	}
	return billingErr, &metered
}

func countRealtimeUsage(info *relaycommon.RelayInfo, event dto.RealtimeEvent, usage *dto.RealtimeUsage, input bool) error {
	text, audio, err := service.CountTokenRealtime(info, event, info.UpstreamModelName)
	if err != nil {
		return fmt.Errorf("count realtime tokens: %w", err)
	}
	increment := dto.RealtimeUsage{TotalTokens: text + audio}
	if input {
		increment.InputTokens = increment.TotalTokens
		increment.InputTokenDetails.TextTokens = text
		increment.InputTokenDetails.AudioTokens = audio
	} else {
		increment.OutputTokens = increment.TotalTokens
		increment.OutputTokenDetails.TextTokens = text
		increment.OutputTokenDetails.AudioTokens = audio
	}
	return addRealtimeUsage(usage, increment)
}

// addRealtimeUsage checks all counters before publishing the new total so an
// invalid upstream count cannot wrap or erase previously metered usage.
func addRealtimeUsage(total *dto.RealtimeUsage, increment dto.RealtimeUsage) error {
	fields := [...]struct {
		counter *int
		amount  int
	}{
		{&total.TotalTokens, increment.TotalTokens},
		{&total.InputTokens, increment.InputTokens},
		{&total.OutputTokens, increment.OutputTokens},
		{&total.InputTokenDetails.CachedTokens, increment.InputTokenDetails.CachedTokens},
		{&total.InputTokenDetails.TextTokens, increment.InputTokenDetails.TextTokens},
		{&total.InputTokenDetails.AudioTokens, increment.InputTokenDetails.AudioTokens},
		{&total.OutputTokenDetails.TextTokens, increment.OutputTokenDetails.TextTokens},
		{&total.OutputTokenDetails.AudioTokens, increment.OutputTokenDetails.AudioTokens},
	}
	for _, field := range fields {
		if field.amount < 0 || *field.counter > math.MaxInt-field.amount {
			return fmt.Errorf("invalid realtime usage token count")
		}
	}
	for _, field := range fields {
		*field.counter += field.amount
	}
	return nil
}
