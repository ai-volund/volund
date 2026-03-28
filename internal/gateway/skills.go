package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/ai-volund/volund/internal/dispatch"
)

// StaticProfileResolver maps profile names to pre-configured model settings
// and skill specs. Loaded from a JSON config file at startup. This is the
// initial implementation for local dev; production will resolve from the
// DB/Forge registry.
type StaticProfileResolver struct {
	profiles map[string]*profileEntry
}

type profileEntry struct {
	Config *dispatch.ProfileConfig
	Skills []dispatch.SkillSpec
}

type profileJSON struct {
	Model struct {
		Provider    string  `json:"provider"`
		Name        string  `json:"name"`
		MaxTokens   int32   `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	} `json:"model"`
	MaxToolRounds int                `json:"max_tool_rounds"`
	SystemPrompt  string             `json:"system_prompt"`
	Skills        []dispatch.SkillSpec `json:"skills"`
}

// NewStaticProfileResolver creates a resolver from a JSON config file.
// Returns a no-op resolver if the file doesn't exist or can't be parsed.
func NewStaticProfileResolver(configPath string) *StaticProfileResolver {
	r := &StaticProfileResolver{
		profiles: make(map[string]*profileEntry),
	}

	if configPath == "" {
		return r
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		slog.Info("skill config not found, no profiles will be loaded", "path", configPath)
		return r
	}

	var cfg struct {
		Profiles map[string]profileJSON `json:"profiles"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("failed to parse skill config", "path", configPath, "error", err)
		return r
	}

	totalSkills := 0
	for name, p := range cfg.Profiles {
		entry := &profileEntry{
			Config: &dispatch.ProfileConfig{
				Provider:      p.Model.Provider,
				Model:         p.Model.Name,
				MaxTokens:     p.Model.MaxTokens,
				Temperature:   p.Model.Temperature,
				MaxToolRounds: p.MaxToolRounds,
				SystemPrompt:  p.SystemPrompt,
			},
			Skills: p.Skills,
		}
		r.profiles[name] = entry
		totalSkills += len(p.Skills)
	}

	slog.Info("loaded profile config",
		"path", configPath,
		"profiles", len(r.profiles),
		"total_skills", totalSkills,
	)

	return r
}

// ResolveProfile returns the model config and skill specs for the given profile.
func (r *StaticProfileResolver) ResolveProfile(_ context.Context, profileName string) (*dispatch.ProfileConfig, []dispatch.SkillSpec, error) {
	entry, ok := r.profiles[profileName]
	if !ok {
		return nil, nil, nil
	}
	return entry.Config, entry.Skills, nil
}
