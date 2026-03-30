package usage

// modelCosts maps model names to per-token costs in USD.
// Input and output costs differ for most models.
type modelCost struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

var costs = map[string]modelCost{
	// OpenAI
	"gpt-4o":             {InputPerMillion: 2.50, OutputPerMillion: 10.00},
	"gpt-4o-mini":        {InputPerMillion: 0.15, OutputPerMillion: 0.60},
	"gpt-4.1":            {InputPerMillion: 2.00, OutputPerMillion: 8.00},
	"gpt-4.1-mini":       {InputPerMillion: 0.40, OutputPerMillion: 1.60},
	"gpt-4.1-nano":       {InputPerMillion: 0.10, OutputPerMillion: 0.40},
	"gpt-4-turbo":        {InputPerMillion: 10.00, OutputPerMillion: 30.00},
	"gpt-3.5-turbo":      {InputPerMillion: 0.50, OutputPerMillion: 1.50},
	"o1":                 {InputPerMillion: 15.00, OutputPerMillion: 60.00},
	"o1-mini":            {InputPerMillion: 1.10, OutputPerMillion: 4.40},
	"o1-pro":             {InputPerMillion: 150.00, OutputPerMillion: 600.00},
	"o3":                 {InputPerMillion: 10.00, OutputPerMillion: 40.00},
	"o3-mini":            {InputPerMillion: 1.10, OutputPerMillion: 4.40},
	"o4-mini":            {InputPerMillion: 1.10, OutputPerMillion: 4.40},

	// Anthropic
	"claude-opus-4":       {InputPerMillion: 15.00, OutputPerMillion: 75.00},
	"claude-sonnet-4":     {InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-opus-4-5":     {InputPerMillion: 15.00, OutputPerMillion: 75.00},
	"claude-sonnet-4-5":   {InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-haiku-3-5":    {InputPerMillion: 0.80, OutputPerMillion: 4.00},

	// Google
	"gemini-2.5-pro":     {InputPerMillion: 1.25, OutputPerMillion: 10.00},
	"gemini-2.5-flash":   {InputPerMillion: 0.15, OutputPerMillion: 0.60},
	"gemini-2.0-flash":   {InputPerMillion: 0.10, OutputPerMillion: 0.40},

	// Embeddings
	"text-embedding-3-small": {InputPerMillion: 0.02, OutputPerMillion: 0},
	"text-embedding-3-large": {InputPerMillion: 0.13, OutputPerMillion: 0},
}

// CalculateCost estimates USD cost for a model usage.
// Returns nil if the model is unknown.
func CalculateCost(model string, inputTokens, outputTokens int) *float64 {
	c, ok := costs[model]
	if !ok {
		return nil
	}
	cost := float64(inputTokens)/1_000_000*c.InputPerMillion +
		float64(outputTokens)/1_000_000*c.OutputPerMillion
	return &cost
}
