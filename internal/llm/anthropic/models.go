package anthropic

import "github.com/ai-volund/volund/internal/llm"

const ProviderName = "anthropic"

// knownModels is the Volund catalog of supported Anthropic models.
// Prices are USD per 1M tokens as of Q1 2025.
var knownModels = []*llm.ModelInfo{
	{
		Provider:           ProviderName,
		ID:                 "claude-opus-4-5",
		DisplayName:        "Claude Opus 4.5",
		ContextWindow:      200_000,
		MaxOutputTokens:    32_000,
		SupportsVision:     true,
		SupportsToolUse:    true,
		SupportsStreaming:   true,
		InputPricePerMTok:  15.00,
		OutputPricePerMTok: 75.00,
	},
	{
		Provider:           ProviderName,
		ID:                 "claude-sonnet-4-5",
		DisplayName:        "Claude Sonnet 4.5",
		ContextWindow:      200_000,
		MaxOutputTokens:    64_000,
		SupportsVision:     true,
		SupportsToolUse:    true,
		SupportsStreaming:   true,
		InputPricePerMTok:  3.00,
		OutputPricePerMTok: 15.00,
	},
	{
		Provider:           ProviderName,
		ID:                 "claude-haiku-4-5",
		DisplayName:        "Claude Haiku 4.5",
		ContextWindow:      200_000,
		MaxOutputTokens:    16_000,
		SupportsVision:     true,
		SupportsToolUse:    true,
		SupportsStreaming:   true,
		InputPricePerMTok:  0.80,
		OutputPricePerMTok: 4.00,
	},
	{
		Provider:           ProviderName,
		ID:                 "claude-opus-4-6",
		DisplayName:        "Claude Opus 4.6",
		ContextWindow:      200_000,
		MaxOutputTokens:    32_000,
		SupportsVision:     true,
		SupportsToolUse:    true,
		SupportsStreaming:   true,
		InputPricePerMTok:  15.00,
		OutputPricePerMTok: 75.00,
	},
	{
		Provider:           ProviderName,
		ID:                 "claude-sonnet-4-6",
		DisplayName:        "Claude Sonnet 4.6",
		ContextWindow:      200_000,
		MaxOutputTokens:    64_000,
		SupportsVision:     true,
		SupportsToolUse:    true,
		SupportsStreaming:   true,
		InputPricePerMTok:  3.00,
		OutputPricePerMTok: 15.00,
	},
}
