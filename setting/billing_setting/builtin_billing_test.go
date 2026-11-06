package billing_setting_test

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPT6AstraBuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	savedOptions := common.OptionMap
	t.Cleanup(func() {
		*settings, common.OptionMap = saved, savedOptions
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	common.OptionMap = map[string]string{"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`}
	require.NoError(t, config.GlobalConfig.LoadFromDB(common.OptionMap))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("gpt-6-astra"))
	expression, ok := billing_setting.GetBillingExpr("gpt-6-astra")
	require.True(t, ok)

	for _, tc := range []struct {
		name                                       string
		input, output, cached, written, written12h int
		request                                    string
		quota                                      int
	}{
		{"standard", 1000, 100, 0, 0, 0, `{}`, 7500},
		{"client flex cannot discount standard pricing", 1000, 100, 0, 0, 0, `{"service_tier":"flex"}`, 7500},
		{"cache at context boundary", 272000, 1000, 200000, 20000, 0, `{}`, 510000},
		{"whole request above boundary", 272001, 1000, 200000, 20000, 0, `{}`, 1007510},
		// The prompt caching guide example: 4000 of the 7000 writes are 12h.
		{"12h cache writes", 12000, 0, 4000, 7000, 4000, `{}`, 75750},
		{"12h cache writes above boundary", 300000, 0, 200000, 20000, 10000, `{}`, 1375000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := &dto.Usage{
				PromptTokens: tc.input, CompletionTokens: tc.output,
				PromptTokensDetails: dto.InputTokenDetails{CachedTokens: tc.cached, CacheWriteTokens: tc.written,
					CacheWriteTokensDetails: &dto.CacheWriteTokenDetails{TTL30mTokens: tc.written - tc.written12h, TTL12hTokens: tc.written12h}},
			}
			params := service.BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expression))
			result, err := billingexpr.ComputeTieredQuotaWithRequest(&billingexpr.BillingSnapshot{
				ExprString: expression, GroupRatio: 1, QuotaPerUnit: 500000,
			}, params, billingexpr.RequestInput{Body: []byte(tc.request)})
			require.NoError(t, err)
			assert.Equal(t, tc.quota, result.ActualQuotaAfterGroup)
		})
	}

	t.Run("admin options expose defaults without persisting them", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		controller.GetOptions(ctx)
		var response struct {
			Success bool
			Data    []model.Option
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		found := map[string]string{}
		for _, option := range response.Data {
			if _, ok := common.OptionMap[option.Key]; ok {
				assert.NotContains(t, found, option.Key)
				var values map[string]string
				require.NoError(t, common.UnmarshalJsonStr(option.Value, &values))
				found[option.Key] = values["gpt-6-astra"]
				assert.Equal(t, `{}`, common.OptionMap[option.Key])
			}
		}
		assert.Equal(t, map[string]string{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}, found)
	})

	for _, tc := range []struct {
		name, mode, expr, ratios, prices, wantMode string
	}{
		{"custom expression overrides legacy price", "tiered_expr", "p * 7", `{"gpt-6-astra":8}`, `{}`, "tiered_expr"},
		{"explicit ratio mode", "ratio", "", `{}`, `{}`, "ratio"},
		{"existing free token price", "", "", `{"gpt-6-astra":0}`, `{}`, "ratio"},
		{"existing per-call price", "", "", `{}`, `{"gpt-6-astra":0.1}`, "ratio"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
			if tc.mode != "" {
				settings.BillingMode["gpt-6-astra"] = tc.mode
			}
			if tc.expr != "" {
				settings.BillingExpr["gpt-6-astra"] = tc.expr
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(tc.ratios))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(tc.prices))
			assert.Equal(t, tc.wantMode, billing_setting.GetBillingMode("gpt-6-astra"))
			actual, ok := billing_setting.GetBillingExpr("gpt-6-astra")
			assert.Equal(t, tc.expr, actual)
			assert.Equal(t, tc.expr != "", ok)
		})
	}
}

func TestImageModelBuiltinPricesAndOverrides(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		*settings = saved
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	for _, name := range []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"} {
		t.Run(name, func(t *testing.T) {
			*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			expression, ok := billing_setting.GetBillingExpr(name)
			require.True(t, ok)
			usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 100, PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens: 300, ImageTokens: 600, CachedTokensDetails: &dto.CachedTokenDetails{ImageTokens: common.GetPointer(200)},
			}}
			result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1, QuotaPerUnit: 500000},
				service.BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expression)))
			require.NoError(t, err)
			assert.Equal(t, 4113, result.ActualQuotaAfterGroup)
			encoded, err := common.Marshal(map[string]float64{name: 0})
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(encoded)))
			assert.Equal(t, "ratio", billing_setting.GetBillingMode(name))
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encoded)))
			assert.Equal(t, "ratio", billing_setting.GetBillingMode(name))
			settings.BillingMode[name] = "tiered_expr"
			settings.BillingExpr[name] = `tier("custom", p * 7)`
			actual, ok := billing_setting.GetBillingExpr(name)
			require.True(t, ok)
			assert.Equal(t, settings.BillingExpr[name], actual)
		})
	}
}

// DeepSeek prices are peak/off-peak expressions in USD per 1M tokens; peak is
// about twice off-peak. The same usage settles to the same quota from a Chat,
// Responses or Anthropic response, and the Search/Fetch routing names cost
// $0.75 per 1,000 requests with no token price. Which tier runs depends on the clock; both are pinned here.
func TestDeepSeekBuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		*settings = saved
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))

	// 1000 cache-miss + 2000 cache-hit input tokens and 500 output tokens.
	chat := &dto.Usage{PromptTokens: 3000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 2000}}
	responses := &dto.Usage{}
	service.ApplyResponsesUsage(responses, &dto.Usage{InputTokens: 3000, OutputTokens: 500, InputTokensDetails: &dto.InputTokenDetails{CachedTokens: 2000}})
	anthropic := &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 2000}, UsageSemantic: "anthropic"}
	quota := func(expression string, usage *dto.Usage) billingexpr.TieredResult {
		params := service.BuildTieredTokenParams(usage, usage.UsageSemantic == "anthropic", billingexpr.UsedVars(expression))
		result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
			ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1, QuotaPerUnit: 500000,
		}, params)
		require.NoError(t, err)
		return result
	}
	for model, want := range map[string]map[string]int{
		// flash peak: 1000*0.2899 + 2000*0.0058 + 500*1.16 = 881.5 -> $0.0008815.
		// flash off-peak: 1000*0.1449 + 2000*0.0029 + 500*0.5797 = 440.55 -> $0.00044055.
		"deepseek-flash":               {"off_peak": 220, "peak": 441},
		"deepseek-v4-flash":            {"off_peak": 220, "peak": 441},
		"deepseek-v4-flash-vision-exp": {"off_peak": 220, "peak": 441},
		// pro peak: 1000*1.3 + 2000*0.0435 + 500*3.91 = 3342 -> $0.003342.
		// pro off-peak: 1000*0.6522 + 2000*0.0217 + 500*1.96 = 1675.6 -> $0.0016756.
		"deepseek-v4-pro": {"off_peak": 838, "peak": 1671},
	} {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(model))
			expression, ok := billing_setting.GetBillingExpr(model)
			require.True(t, ok)
			for name, usage := range map[string]*dto.Usage{"chat": chat, "responses": responses, "anthropic": anthropic} {
				result := quota(expression, usage)
				require.Contains(t, want, result.MatchedTier, name)
				assert.Equal(t, want[result.MatchedTier], result.ActualQuotaAfterGroup, name)
			}
		})
	}
	for _, model := range []string{"deepseek-search", "deepseek-fetch"} {
		expression, ok := billing_setting.GetBillingExpr(model)
		require.True(t, ok, model)
		result := quota(expression, &dto.Usage{})
		assert.Equal(t, billingexpr.BillingUnitRequest, result.BillingUnit, model)
		assert.Equal(t, 375, result.ActualQuotaAfterGroup, model)
	}
}

// GPT-6 Luna and Sol are flat USD per 1M tokens. The cached usage reports
// 3000 prompt tokens including 1000 cache reads and 800 cache writes.
func TestGPT6LunaSolBuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		*settings = saved
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))

	plain := &dto.Usage{PromptTokens: 1000, CompletionTokens: 100}
	cached := &dto.Usage{
		PromptTokens: 3000, CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 1000, CacheWriteTokens: 800},
	}
	for model, want := range map[string]struct{ plain, cached int }{
		// Luna plain: 1000*0.1 + 100*0.5 = 150 -> $0.00015.
		// Luna cached: 1200*0.1 + 1000*0.01 + 800*0.125 + 200*0.5 = 330 -> $0.00033.
		"gpt-6-luna": {plain: 75, cached: 165},
		// Sol plain: 1000*2 + 100*10 = 3000 -> $0.003.
		// Sol cached: 1200*2 + 1000*0.2 + 800*2.5 + 200*10 = 6600 -> $0.0066.
		"gpt-6-sol": {plain: 1500, cached: 3300},
	} {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(model))
			expression, ok := billing_setting.GetBillingExpr(model)
			require.True(t, ok)
			for name, tc := range map[string]struct {
				usage *dto.Usage
				quota int
			}{
				"plain":  {plain, want.plain},
				"cached": {cached, want.cached},
			} {
				params := service.BuildTieredTokenParams(tc.usage, false, billingexpr.UsedVars(expression))
				result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
					ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1, QuotaPerUnit: 500000,
				}, params)
				require.NoError(t, err, name)
				assert.Equal(t, tc.quota, result.ActualQuotaAfterGroup, name)
			}
		})
	}
}

// Claude 5 family built-in prices are flat USD per 1M tokens: input, output,
// cache read at 0.1x input, 5m cache write at 1.25x and 1h cache write at 2x.
// Hosted web search is billed by the tool price, not by the expression.
func TestClaude5BuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		*settings = saved
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))

	plain := &dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic"}
	// 1000 text input, 2000 cache reads, 400 5m writes and 200 1h writes.
	cached := &dto.Usage{
		PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic",
		PromptTokensDetails:         dto.InputTokenDetails{CachedTokens: 2000, CacheWriteTokens: 600},
		ClaudeCacheCreation5mTokens: 400, ClaudeCacheCreation1hTokens: 200,
	}
	for model, want := range map[string]struct{ plain, cached int }{
		// Opus: 1000*5 + 100*25 = 7500 -> $0.0075; with cache 13000 -> $0.013.
		"claude-opus-5-5": {plain: 3750, cached: 6500},
		// Sonnet: 1000*2 + 100*10 = 3000 -> $0.003; with cache 5200 -> $0.0052.
		"claude-sonnet-5-5": {plain: 1500, cached: 2600},
		// Fable: 1000*10 + 100*50 = 15000 -> $0.015; with cache 26000 -> $0.026.
		"claude-fable-5-1": {plain: 7500, cached: 13000},
	} {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(model))
			expression, ok := billing_setting.GetBillingExpr(model)
			require.True(t, ok)
			for name, tc := range map[string]struct {
				usage *dto.Usage
				quota int
			}{
				"plain":  {plain, want.plain},
				"cached": {cached, want.cached},
			} {
				params := service.BuildTieredTokenParams(tc.usage, true, billingexpr.UsedVars(expression))
				result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
					ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1, QuotaPerUnit: 500000,
				}, params)
				require.NoError(t, err, name)
				assert.Equal(t, tc.quota, result.ActualQuotaAfterGroup, name)
			}
		})
	}
}
