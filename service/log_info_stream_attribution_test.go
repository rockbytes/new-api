package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamLogRecordsErrorAttributionOnlyForAbnormalEnds(t *testing.T) {
	tests := []struct {
		name      string
		endReason relaycommon.StreamEndReason
		wantAdmin bool
		source    string
		stage     string
	}{
		{"caller disconnected is recorded for administrators", relaycommon.StreamEndReasonClientGone, true, "client", "stream"},
		{"upstream timeout is recorded for administrators", relaycommon.StreamEndReasonTimeout, true, "network", "stream"},
		{"clean completion is not attributed", relaycommon.StreamEndReasonDone, false, "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(test.endReason, nil)
			info := &relaycommon.RelayInfo{IsStream: true, StreamStatus: status}
			other := model.NewLogOther()

			appendStreamStatus(info, other)

			stored, err := common.StrToMap(other.JSONString())
			require.NoError(t, err)
			adminInfo, hasAdmin := stored["admin_info"].(map[string]any)
			assert.Equal(t, test.wantAdmin, hasAdmin)
			if test.wantAdmin {
				assert.Equal(t, test.source, adminInfo["error_source"])
				assert.Equal(t, test.stage, adminInfo["error_stage"])
			}
		})
	}
}
