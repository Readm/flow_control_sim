// Package configconv provides generic converters between Protocol structs and maps.
// It uses "github.com/mitchellh/mapstructure" for robust decoding and
// standard "encoding/json" for encoding, ensuring consistent behavior with JSON tags.
package configconv

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/mitchellh/mapstructure"
)

// StructToMap converts any Protocol Config struct to map[string]interface{}.
// It uses JSON marshaling/unmarshaling to ensure faithful respect of `json` tags.
func StructToMap(obj interface{}) map[string]interface{} {
	if obj == nil {
		return make(map[string]interface{})
	}
	// Check for nil pointer
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return make(map[string]interface{})
	}

	// Marshal to JSON
	b, err := json.Marshal(obj)
	if err != nil {
		// Should not happen for Protocol constants/structs
		return make(map[string]interface{})
	}

	// Unmarshal back to map
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return make(map[string]interface{})
	}

	if m == nil {
		return make(map[string]interface{})
	}
	return m
}

// MapToStruct converts map[string]interface{} to any Protocol Config struct.
// It uses mapstructure for robust weak type decoding (e.g. int -> int64, string -> int).
func MapToStruct(data map[string]interface{}, obj interface{}) error {
	if data == nil {
		return nil
	}

	config := &mapstructure.DecoderConfig{
		TagName:          "json", // Use existing JSON tags
		Result:           obj,
		WeaklyTypedInput: true, // Allow automatic type conversion (int -> float, string -> int)
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	return decoder.Decode(data)
}

// MergeMaps merges two maps recursively.
// statsMap values override configMap values.
// Nested maps are merged deeply, ensuring other fields in configMap are preserved.
func MergeMaps(configMap, statsMap map[string]interface{}) map[string]interface{} {
	return deepMerge(configMap, statsMap)
}

func deepMerge(dst, src map[string]interface{}) map[string]interface{} {
	// Start with a copy of dst to avoid mutating the original
	out := make(map[string]interface{}, len(dst))
	for k, v := range dst {
		out[k] = v
	}

	for k, v := range src {
		// If both are maps, merge recursively
		srcMap, srcOk := v.(map[string]interface{})
		dstVal, dstExists := out[k]
		dstMap, dstOk := dstVal.(map[string]interface{})

		if srcOk && dstExists && dstOk {
			out[k] = deepMerge(dstMap, srcMap)
		} else {
			// Otherwise overwrite
			out[k] = v
		}
	}

	return out
}
