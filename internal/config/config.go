// Package config loads, merges, overlays, and persists Clash/mihomo
// configuration documents while preserving keys this project does not know
// about.
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse decodes a YAML mapping. Values are kept as generic Go values so that
// unknown or future configuration keys survive a load/save cycle.
func Parse(data []byte) (map[string]any, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}

	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	cfg, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("YAML document must be a mapping, got %T", value)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

// Load reads and parses a YAML configuration file.
func Load(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}
	return cfg, nil
}

// Marshal encodes a configuration as YAML.
func Marshal(cfg map[string]any) ([]byte, error) {
	if cfg == nil {
		cfg = map[string]any{}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return data, nil
}

// Clone makes a deep copy of a generic configuration tree. Decoded YAML uses
// map[string]any and []any, which are the only containers handled specially.
func Clone(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return Clone(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

// MarshalIndent encodes a configuration with two-space indentation.
func MarshalIndent(cfg map[string]any) ([]byte, error) {
	if cfg == nil {
		cfg = map[string]any{}
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("finish config encoding: %w", err)
	}
	return buf.Bytes(), nil
}
