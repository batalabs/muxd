package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/batalabs/muxd/internal/domain"
)

// DefaultPricingMap returns the built-in pricing for known models.
func DefaultPricingMap() map[string]domain.ModelPricing {
	return map[string]domain.ModelPricing{
		// Anthropic
		"claude-opus-4-6":            {InputPerMillion: 5.0, OutputPerMillion: 25.0},
		"claude-opus-4-5-20251101":   {InputPerMillion: 5.0, OutputPerMillion: 25.0},
		"claude-opus-4-1-20250805":   {InputPerMillion: 15.0, OutputPerMillion: 75.0},
		"claude-opus-4-20250514":     {InputPerMillion: 15.0, OutputPerMillion: 75.0},
		"claude-sonnet-4-6":          {InputPerMillion: 3.0, OutputPerMillion: 15.0},
		"claude-sonnet-4-5-20250929": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
		"claude-sonnet-4-20250514":   {InputPerMillion: 3.0, OutputPerMillion: 15.0},
		"claude-3-7-sonnet-20250219": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
		"claude-haiku-4-5-20251001":  {InputPerMillion: 1.0, OutputPerMillion: 5.0},
		"claude-3-5-haiku-20241022":  {InputPerMillion: 0.80, OutputPerMillion: 4.0},
		"claude-3-haiku-20240307":    {InputPerMillion: 0.25, OutputPerMillion: 1.25},
		// OpenAI
		"gpt-4o":      {InputPerMillion: 2.50, OutputPerMillion: 10.0},
		"gpt-4o-mini": {InputPerMillion: 0.15, OutputPerMillion: 0.60},
		"o3":          {InputPerMillion: 10.0, OutputPerMillion: 40.0},
		"o3-mini":     {InputPerMillion: 1.10, OutputPerMillion: 4.40},
		"o4-mini":     {InputPerMillion: 1.10, OutputPerMillion: 4.40},
	}
}

// LoadPricing reads pricing from ~/.config/muxd/pricing.json.
// Missing entries are filled from defaults; the merged result is written back
// so newly added models appear in the file for the user to edit.
func LoadPricing() map[string]domain.ModelPricing {
	defaults := DefaultPricingMap()

	dir := ConfigDir()
	if dir == "" {
		return defaults
	}

	data, err := os.ReadFile(filepath.Join(dir, "pricing.json"))
	if err != nil {
		// First run or missing file — write defaults.
		if err := SavePricing(defaults); err != nil {
			fmt.Fprintf(os.Stderr, "config: save default pricing: %v\n", err)
		}
		return defaults
	}

	loaded := make(map[string]domain.ModelPricing)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return defaults
	}

	// Merge: user values win, but add any new defaults they don't have.
	changed := false
	for k, v := range defaults {
		if _, ok := loaded[k]; !ok {
			loaded[k] = v
			changed = true
		}
	}
	if changed {
		if err := SavePricing(loaded); err != nil {
			fmt.Fprintf(os.Stderr, "config: save merged pricing: %v\n", err)
		}
	}
	return loaded
}

// SavePricing writes pricing to ~/.config/muxd/pricing.json.
func SavePricing(m map[string]domain.ModelPricing) error {
	dir := ConfigDir()
	if dir == "" {
		return fmt.Errorf("could not determine config directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling pricing: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "pricing.json"), data, 0o644)
}
