package relay

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func WssHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.TieredBillingSnapshot == nil {
		// A failed dial may have selected a different auto group on retry.
		info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	//var requestBody io.Reader
	//firstWssRequest, _ := c.Get("first_wss_request")
	//requestBody = bytes.NewBuffer(firstWssRequest.([]byte))

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed)
	}

	if resp != nil {
		info.TargetWs = resp.(*websocket.Conn)
		defer info.TargetWs.Close()
		var closeOnce sync.Once
		unregister := wsmanager.Register(info.ChannelId, wsmanager.KindRealtime, func(reason string) {
			closeOnce.Do(func() {
				deadline := time.Now().Add(time.Second)
				closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
				_ = info.ClientWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.TargetWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.ClientWs.Close()
				_ = info.TargetWs.Close()
			})
		})
		defer unregister()
	}

	usage, newAPIError := adaptor.DoResponse(c, nil, info)
	if meteredUsage, ok := usage.(*service.MeteredRealtimeUsage); ok && meteredUsage != nil {
		// Settle admitted usage even if a later turn was rejected. The common
		// failure cleanup will not refund a settled session.
		if err := meteredUsage.Settle(c, info, info.UpstreamModelName); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		// A running websocket cannot be replayed against another channel.
		return newAPIError
	}
	if newAPIError != nil {
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	return types.NewError(fmt.Errorf("missing realtime usage"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
}
