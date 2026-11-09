package billing_setting

import "github.com/QuantumNous/new-api/constant"

// deepSeekPeak is DeepSeek's peak window: Monday to Friday, 09:00-12:00 and
// 14:00-18:00 Beijing time. The Chinese public holidays DeepSeek bills
// off-peak are not known in advance and bill at peak here.
const deepSeekPeak = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))`

// deepSeekFlashExpr prices DeepSeek-V4.1-Flash, the standard Flash model:
// peak 高峰 and off-peak 空闲 USD per 1M tokens for uncached input, cache hit
// input and output.
const deepSeekFlashExpr = deepSeekPeak + ` ? tier("peak", p * 0.2899 + cr * 0.0058 + c * 1.16) : tier("off_peak", p * 0.1449 + cr * 0.0029 + c * 0.5797)`

// Built-in token prices use actual USD per million tokens. Keep new model
// defaults here instead of splitting them across the legacy ratio tables.
var builtinBillingExpr = map[string]string{
	// https://developers.openai.com/api/docs/pricing (Standard, 2026-09-09).
	// The Images API reports image output in output_tokens, normalized to c.
	"gpt-image-2":            `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-sunburst": `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-flare":    `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	// https://developers.openai.com/api/docs/models/gpt-6-astra
	// https://developers.openai.com/api/docs/pricing (Standard, 2026-10-22):
	// cc is the 30m cache write and cc12h the 12h write. The long-context
	// rates apply to the whole request.
	// Do not infer service-tier discounts from incoming request parameters:
	// channels filter service_tier by default, so it may not reach the upstream.
	"gpt-6-astra": `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5 + cc12h * 25) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25 + cc12h * 50)`,
	// Claude 5 family, USD per 1M tokens, flat across the 1M context window.
	// cc is the 5m cache write, cc1h the 1h write (2x input). Hosted web search
	// is not in the expression: it bills per call through the default
	// web_search tool price ($10 per 1K calls).
	"claude-opus-5-5":   `tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)`,
	"claude-sonnet-5-5": `tier("base", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`,
	"claude-fable-5-1":  `tier("base", p * 10 + c * 50 + cr * 1 + cc * 12.5 + cc1h * 20)`,
	// GPT-6 Luna: flat USD per 1M tokens across the 1.05M context
	// window, from the model price list (cache write is the single write
	// price, so no 12h split is priced here). Hosted web search bills per call
	// through the default web_search tool price ($10 per 1K calls).
	"gpt-6-luna": `tier("base", p * 0.1 + c * 0.5 + cr * 0.01 + cc * 0.125)`,
	// GPT-6 Sol: USD per 1M tokens. Once input exceeds 272K tokens, the
	// long-context rates apply to the whole request. Cache write is the single
	// write price, so no 12h split is priced here. Hosted web search bills per
	// call through the default web_search tool price ($10 per 1K calls).
	"gpt-6-sol": `len <= 272000 ? tier("standard", p * 2 + c * 10 + cr * 0.2 + cc * 2.5) : tier("long_context", p * 4 + c * 15 + cr * 0.4 + cc * 5)`,
	// DeepSeek standard V4.1 Flash and V4 Pro peak/off-peak prices (USD per 1M
	// tokens), from the standard price list. deepseek-v4-flash and
	// deepseek-v4-flash-vision-exp run on V4.1 Flash and bill at the Flash
	// price. Images bill as input tokens, and there is no cache-write price.
	"deepseek-flash":               deepSeekFlashExpr,
	"deepseek-v4-flash":            deepSeekFlashExpr,
	"deepseek-v4-flash-vision-exp": deepSeekFlashExpr,
	"deepseek-v4-pro":              deepSeekPeak + ` ? tier("peak", p * 1.3 + cr * 0.0435 + c * 3.91) : tier("off_peak", p * 0.6522 + cr * 0.0217 + c * 1.96)`,
	// Gemini standard paid-tier prices, USD per 1M tokens, from the price list
	// for these models. Each has a flat 1M context window. Image, audio and
	// video inputs bill at the text input price. Hosted Google Search bills per
	// query through the default google_search tool price ($14 per 1K queries).
	"gemini-3.5-flash":      `tier("base", p * 1.5 + c * 9 + cr * 0.15)`,
	"gemini-3.5-flash-lite": `tier("base", p * 0.3 + c * 2.5 + cr * 0.03)`,
	"gemini-4-argon":        `tier("base", p * 2 + c * 10 + cr * 0.1)`,
	// gemini-2.5-flash-lite has no cache read price, so cached tokens bill at
	// the input price. Batch prices are not modeled.
	"gemini-2.5-flash-lite": `tier("base", p * 0.1 + c * 0.4)`,
	// The Search and Fetch API routing names: $0.75 per 1,000 successful
	// requests each, the same pricing page. The calls carry no tokens.
	constant.DeepSeekSearchModel: `tier("request", fixed(0.00075))`,
	constant.DeepSeekFetchModel:  `tier("request", fixed(0.00075))`,
}
