package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const payerTestOrganizationKey = "payerhttporganizationkey"
const payerTestPersonalKey = "payerhttppersonalkey"

// setupOrganizationPayerHTTP runs the real relay router against an isolated database and
// a fake OpenAI-compatible upstream. The member pays through an organization-bound Key and
// keeps a personal wallet for a personal Key.
func setupOrganizationPayerHTTP(t *testing.T) (*httptest.Server, int, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousLogConsume := common.RedisEnabled, common.LogConsumeEnabled
	previousSQLitePath, previousMaster := common.SQLitePath, common.IsMasterNode
	previousCount, previousMedia := constant.CountToken, constant.GetMediaToken
	previousStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	common.SQLitePath = t.TempDir() + "/payer-http.db"
	common.RedisEnabled = false
	common.LogConsumeEnabled = true
	common.IsMasterNode = true
	// The server counts tokens and media by default; the test must do the same so that the
	// audio request has a billable quantity.
	constant.CountToken, constant.GetMediaToken = true, true
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	// InitDB is the production startup path: it runs the migrations and sets the column names
	// that the queries use, which the test cannot set directly.
	// Startup order matches main.go: default ratios first, then the database, then options.
	ratio_setting.InitRatioSettings()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecialGroups := specialGroups.ReadAll()
	t.Cleanup(func() {
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})
	// The payer members use the default user group, which may select the default group.
	specialGroups.Set("default", map[string]string{"+:default": "Default"})
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	model.InitOptionMap()
	// Options reset the logging switch, so set it after they are loaded.
	common.LogConsumeEnabled = true
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.UserSubscription{}, &model.OrganizationMemberQuota{}, &model.OrganizationQuotaLedgerEntry{}))

	// Streaming keep-alive needs a positive ping interval, which the server loads from options.
	generalSetting := operation_setting.GetGeneralSetting()
	previousPing := *generalSetting
	generalSetting.PingIntervalEnabled, generalSetting.PingIntervalSeconds = true, 10
	t.Cleanup(func() { *generalSetting = previousPing })
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(payerTestUpstream))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.LogConsumeEnabled = previousRedis, previousLogConsume
		common.SQLitePath, common.IsMasterNode = previousSQLitePath, previousMaster
		constant.CountToken, constant.GetMediaToken = previousCount, previousMedia
		constant.StreamingTimeout = previousStreamingTimeout
		common.SetDatabaseTypes(previousMain, previousLog)
	})

	member := &model.User{Id: 30, Username: "payer-http-member", Password: "not-a-real-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "aff-payer-http-member", Quota: 1000000}
	require.NoError(t, db.Create(member).Error)
	organization := &model.Organization{Name: "Payer HTTP Co", Status: model.OrganizationStatusActive, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(organization).Error)
	require.NoError(t, db.Create(&model.OrganizationMembership{OrganizationId: organization.Id, UserId: member.Id,
		Role: model.OrganizationRoleMember, Status: model.OrganizationMembershipActive, CreatedTime: 1, UpdatedTime: 1}).Error)
	_, err := model.GrantOrganizationQuota(organization.Id, 10000000, 1, "grant", "grant-payer-http", 1)
	require.NoError(t, err)
	require.NoError(t, model.SetOrganizationMemberAllocation(organization.Id, member.Id, 5000000, 2))

	organizationKey := &model.Token{UserId: member.Id, Key: payerTestOrganizationKey, Name: "payer-org", Status: common.TokenStatusEnabled,
		CreatedTime: 1, AccessedTime: 1, ExpiredTime: -1, UnlimitedQuota: true, AllowUserQuotaQuery: true, Group: "default"}
	require.NoError(t, db.Create(organizationKey).Error)
	_, err = model.BindTokenBillingOrganization(organizationKey.Id, member.Id, &organization.Id)
	require.NoError(t, err)
	personalKey := &model.Token{UserId: member.Id, Key: payerTestPersonalKey, Name: "payer-personal", Status: common.TokenStatusEnabled,
		CreatedTime: 1, AccessedTime: 1, ExpiredTime: -1, UnlimitedQuota: true, AllowUserQuotaQuery: true, Group: "default"}
	require.NoError(t, db.Create(personalKey).Error)
	_, err = model.GetTokenByKey(payerTestOrganizationKey, true)
	require.NoError(t, err, "the organization Key must be stored under its name without the sk- prefix")

	baseURL := upstream.URL
	channel := &model.Channel{Name: "payer-http-upstream", Type: constant.ChannelTypeOpenAI, Key: "sk-upstream", BaseURL: &baseURL,
		Models: "gpt-4o-mini,gpt-4o-realtime-preview,dall-e-3,tts-1,mj_imagine,mj_inpaint,swap_face", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.Insert())

	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	// /images/generations is a host protocol route, registered alongside the relay routes.
	SetTaskPluginProtocolRouter(engine)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	return gateway, organization.Id, member
}

// payerTestUpstream answers the four request shapes with fixed usage, so each charge is known.
// A streaming chat request gets server-sent events that end with the usage chunk.
func payerTestUpstream(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/chat/completions":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"id":"c","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"id":"c","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	case "/v1/images/generations":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"url":"http://127.0.0.1/img.png"}]}`)
	case "/v1/audio/speech":
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "fake-audio")
	case "/v1/realtime":
		upgrader := websocket.Upgrader{}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"usage":{"total_tokens":1,"input_tokens":1,"input_token_details":{"text_tokens":1}}}}`)); err != nil {
				return
			}
		}
	case "/mj/submit/imagine", "/mj/submit/change", "/mj/insight-face/swap":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Midjourney-Upstream", "called")
		_, _ = io.WriteString(w, `{"code":1,"description":"upstream accepted","result":"mj-upstream-result"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// TestOrganizationPayerPerRequestPathThroughHTTP sends each request shape through the relay
// router and checks the consume log written for that request. The organization pays for
// every organization-bound Key request, regardless of a payer named in the body, and a
// personal Key stays on the member's wallet.
func TestOrganizationPayerPerRequestPathThroughHTTP(t *testing.T) {
	gateway, organizationId, member := setupOrganizationPayerHTTP(t)
	cases := []struct {
		name, key, path, body string
		wantOrganization      int
	}{
		{"synchronous text", payerTestOrganizationKey, "/v1/chat/completions",
			`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}],"organization_id":999}`, organizationId},
		{"streaming text", payerTestOrganizationKey, "/v1/chat/completions",
			`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`, organizationId},
		{"image generation", payerTestOrganizationKey, "/v1/images/generations",
			`{"model":"dall-e-3","prompt":"a cat","n":1}`, organizationId},
		{"audio", payerTestOrganizationKey, "/v1/audio/speech",
			`{"model":"tts-1","input":"hello","voice":"alloy"}`, organizationId},
		{"personal key", payerTestPersonalKey, "/v1/chat/completions",
			`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before model.User
			require.NoError(t, model.DB.First(&before, member.Id).Error)

			request, err := http.NewRequest(http.MethodPost, gateway.URL+tc.path, strings.NewReader(tc.body))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer sk-"+tc.key)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			responseBody, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			_ = response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode, string(responseBody))
			requestId := response.Header.Get("X-Oneapi-Request-Id")
			require.NotEmpty(t, requestId)

			// The consume log is written after the response is sent, so wait for it.
			var logs []model.Log
			require.Eventually(t, func() bool {
				return model.LOG_DB.Where("request_id = ? AND type = ?", requestId, model.LogTypeConsume).Find(&logs).Error == nil && len(logs) > 0
			}, 5*time.Second, 10*time.Millisecond)
			require.Len(t, logs, 1)
			assert.Equal(t, tc.wantOrganization, logs[0].BillingOrganizationId, "the payer comes from the Key, not from the request body")
			assert.Positive(t, logs[0].Quota)

			var after model.User
			require.NoError(t, model.DB.First(&after, member.Id).Error)
			if tc.wantOrganization == 0 {
				assert.EqualValues(t, before.PersonalKeyUsedQuota+int64(logs[0].Quota), after.PersonalKeyUsedQuota)
				assert.Equal(t, before.Quota-logs[0].Quota, after.Quota, "a personal Key is charged to the member's wallet")
				return
			}
			assert.Equal(t, before.Quota, after.Quota, "an organization-paid request leaves the member's wallet untouched")
			assert.Equal(t, before.PersonalKeyUsedQuota, after.PersonalKeyUsedQuota)
		})
	}

	// The organization's consumed quota is exactly the sum of the quota its logs record.
	reconciliation, err := model.GetOrganizationQuotaReconciliation(organizationId)
	require.NoError(t, err)
	var organizationLogged int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("billing_organization_id = ? AND type = ?", organizationId, model.LogTypeConsume).
		Select("COALESCE(SUM(quota), 0)").Scan(&organizationLogged).Error)
	assert.Equal(t, organizationLogged, reconciliation.ConsumedQuota)
	assert.True(t, reconciliation.Balanced)
}

func TestRealtimeMetersUsageOnceForSelectedPayer(t *testing.T) {
	for _, tc := range []struct {
		name, key, errorCode string
		org                  bool
		allocation, keyQuota int
	}{
		{name: "personal wallet", key: payerTestPersonalKey, keyQuota: 100},
		{name: "organization allocation", key: payerTestOrganizationKey, org: true, keyQuota: 100},
		{name: "allocation runs out", key: payerTestOrganizationKey, org: true, allocation: 3, keyQuota: 100, errorCode: "insufficient_member_quota"},
		{name: "organization key runs out", key: payerTestOrganizationKey, org: true, keyQuota: 3, errorCode: "pre_consume_token_quota_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, organizationID, member := setupOrganizationPayerHTTP(t)
			token, err := model.GetTokenByKey(tc.key, false)
			require.NoError(t, err)
			require.NoError(t, model.DB.Model(token).Updates(map[string]any{"unlimited_quota": false, "remain_quota": tc.keyQuota}).Error)
			if tc.allocation > 0 {
				require.NoError(t, model.SetOrganizationMemberAllocation(organizationID, member.Id, int64(tc.allocation), 2))
			}

			url := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/v1/realtime?model=gpt-4o-realtime-preview"
			headers := http.Header{"Authorization": {"Bearer sk-" + tc.key}}
			ws, _, err := websocket.DefaultDialer.Dial(url, headers)
			require.NoError(t, err)
			defer ws.Close()
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
			for i := range 2 {
				require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
				_, message, err := ws.ReadMessage()
				require.NoError(t, err)
				if tc.errorCode != "" && i == 1 {
					assert.Contains(t, string(message), tc.errorCode)
				} else {
					assert.Contains(t, string(message), "response.done")
				}
			}
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			require.NoError(t, ws.Close())

			// With ratio 2.5, 1 token rounds to 3, but 2 tokens round to 5, not 6.
			wantQuota, wantTokens, wantOrg := 5, 2, 0
			if tc.errorCode != "" {
				wantQuota, wantTokens = 3, 1
			}
			if tc.org {
				wantOrg = organizationID
			}
			var logs []model.Log
			require.Eventually(t, func() bool {
				return model.LOG_DB.Where("model_name = ? AND type = ?", "gpt-4o-realtime-preview", model.LogTypeConsume).Find(&logs).Error == nil && len(logs) > 0
			}, 5*time.Second, 10*time.Millisecond)
			require.Len(t, logs, 1)
			assert.Equal(t, wantOrg, logs[0].BillingOrganizationId)
			assert.Equal(t, wantQuota, logs[0].Quota)
			assert.Equal(t, wantTokens, logs[0].PromptTokens)
			require.NoError(t, model.DB.First(token, token.Id).Error)
			assert.Equal(t, wantQuota, token.UsedQuota)
			assert.Equal(t, tc.keyQuota-wantQuota, token.RemainQuota)
			var user model.User
			require.NoError(t, model.DB.First(&user, member.Id).Error)
			reconciliation, err := model.GetOrganizationQuotaReconciliation(organizationID)
			require.NoError(t, err)
			if tc.org {
				assert.Equal(t, member.Quota, user.Quota)
				assert.Equal(t, int64(wantQuota), reconciliation.ConsumedQuota)
			} else {
				assert.Equal(t, member.Quota-wantQuota, user.Quota)
				assert.Zero(t, reconciliation.ConsumedQuota)
			}
			assert.True(t, reconciliation.Balanced)
		})
	}
}

func TestLegacyMidjourneyRejectsOnlyBillableOrganizationActions(t *testing.T) {
	gateway, organizationID, member := setupOrganizationPayerHTTP(t)
	var channel model.Channel
	require.NoError(t, model.DB.Where("name = ?", "payer-http-upstream").Take(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Midjourney{UserId: member.Id, ChannelId: channel.Id, MjId: "mj-existing", Status: "SUCCESS"}).Error)
	for _, tc := range []struct {
		name, key, method, path, body, wantBody string
		wantStatus                              int
	}{
		{"imagine with organization key", payerTestOrganizationKey, http.MethodPost, "/mj/submit/imagine", `{"prompt":"a cat"}`, "organization_payer_unsupported", http.StatusForbidden},
		{"swap with organization key", payerTestOrganizationKey, http.MethodPost, "/mj/insight-face/swap", `{"sourceBase64":"YQ==","targetBase64":"Yg=="}`, "organization_payer_unsupported", http.StatusForbidden},
		{"task read with organization key", payerTestOrganizationKey, http.MethodGet, "/mj/task/mj-existing/fetch", "", "mj-existing", http.StatusOK},
		{"unbilled inpaint with organization key", payerTestOrganizationKey, http.MethodPost, "/mj/submit/change", `{"taskId":"mj-existing","action":"INPAINT","index":1}`, "upstream accepted", http.StatusOK},
		{"imagine with personal key", payerTestPersonalKey, http.MethodPost, "/mj/submit/imagine", `{"prompt":"a cat"}`, "upstream accepted", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(tc.method, gateway.URL+tc.path, strings.NewReader(tc.body))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer sk-"+tc.key)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, response.StatusCode, string(body))
			assert.Contains(t, string(body), tc.wantBody)
		})
	}
	reconciliation, err := model.GetOrganizationQuotaReconciliation(organizationID)
	require.NoError(t, err)
	assert.Zero(t, reconciliation.ConsumedQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, member.Id).Error)
	assert.Less(t, user.Quota, member.Quota, "only the successful personal submission charges the wallet")
}
