package config

import "strings"

// SelectKey is cx's own top-level selection key. It is removed before a
// configuration is handed to mihomo.
const SelectKey = "x-cx-select"

// SelectSpecs returns the configured node selection specs.
func SelectSpecs(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	value, ok := cfg[SelectKey]
	if !ok {
		return nil
	}
	var specs []string
	switch value := value.(type) {
	case string:
		specs = []string{value}
	case []string:
		specs = value
	case []any:
		for _, item := range value {
			if spec, ok := item.(string); ok {
				specs = append(specs, spec)
			}
		}
	default:
		return nil
	}
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec = strings.TrimSpace(spec); spec != "" {
			out = append(out, spec)
		}
	}
	return out
}

// SetSelectSpecs stores node selection specs in cfg. An empty list removes the
// key.
func SetSelectSpecs(cfg map[string]any, specs []string) {
	if cfg == nil {
		return
	}
	if len(specs) == 0 {
		delete(cfg, SelectKey)
		return
	}
	values := make([]any, 0, len(specs))
	for _, spec := range specs {
		if spec = strings.TrimSpace(spec); spec != "" {
			values = append(values, spec)
		}
	}
	if len(values) == 0 {
		delete(cfg, SelectKey)
		return
	}
	cfg[SelectKey] = values
}

// StripSelect removes cx's selection key before the config is given to mihomo.
func StripSelect(cfg map[string]any) {
	delete(cfg, SelectKey)
}
