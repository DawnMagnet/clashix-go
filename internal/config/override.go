package config

import (
	"errors"
	"fmt"
	"os"
)

// LoadOverride reads an override document. A missing optional override file is
// reported as (nil, false, nil).
func LoadOverride(path string, optional bool) (map[string]any, bool, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, true, nil
	}
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("load override %q: %w", path, err)
}

// MergeOverride deep-merges an override document over base. Maps merge
// recursively; lists and scalars replace the base value.
func MergeOverride(base, override map[string]any) map[string]any {
	result := Clone(base)
	if result == nil {
		result = map[string]any{}
	}
	for key, value := range override {
		result[key] = mergeOverrideValue(result[key], value)
	}
	return result
}

func mergeOverrideValue(base, override any) any {
	baseMap, baseOK := base.(map[string]any)
	overrideMap, overrideOK := override.(map[string]any)
	if baseOK && overrideOK {
		merged := Clone(baseMap)
		if merged == nil {
			merged = map[string]any{}
		}
		for key, value := range overrideMap {
			merged[key] = mergeOverrideValue(merged[key], value)
		}
		return merged
	}
	return cloneValue(override)
}
