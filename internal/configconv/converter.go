// Package configconv provides generic converters between Protocol structs and maps.
// It eliminates code duplication by using reflection to handle any Protocol Config type.
package configconv

import (
	"fmt"
	"reflect"
	"strings"
)

// StructToMap converts any Protocol Config struct to map[string]interface{}.
// It automatically handles:
// - Pointer field dereferencing (*int, *string, *float32)
// - JSON tag mapping for field names
// - Recursive processing of nested structs (e.g., *CacheConfig)
// - Skipping nil pointers (omitempty behavior)
func StructToMap(obj interface{}) map[string]interface{} {
	if obj == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	v := reflect.ValueOf(obj)

	// Handle pointer: if input is *CPUConfig, dereference to CPUConfig
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return result
		}
		v = v.Elem()
	}

	// Must be a struct
	if v.Kind() != reflect.Struct {
		return result
	}

	t := v.Type()

	// Iterate over all fields
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		// Get field name from JSON tag (e.g., `json:"rob_size,omitempty"`)
		jsonTag := fieldType.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		// Parse tag to extract field name (remove omitempty, etc.)
		parts := strings.Split(jsonTag, ",")
		fieldName := parts[0]

		// Get field value
		var value interface{}

		// Handle pointer fields (*int, *string, *float32, *CacheConfig)
		if field.Kind() == reflect.Ptr {
			// Skip nil pointers (corresponds to JSON omitempty)
			if field.IsNil() {
				continue
			}

			// Dereference pointer
			fieldValue := field.Elem()

			// Check if it's a nested struct (e.g., *CacheConfig)
			if fieldValue.Kind() == reflect.Struct {
				// Recursively process nested struct
				value = StructToMap(fieldValue.Interface())
			} else {
				// Simple type: get value directly
				value = fieldValue.Interface()
			}
		} else {
			// Non-pointer field (rare in Protocol, but supported)
			if field.Kind() == reflect.Struct {
				value = StructToMap(field.Interface())
			} else {
				value = field.Interface()
			}
		}

		result[fieldName] = value
	}

	return result
}

// MapToStruct converts map[string]interface{} to any Protocol Config struct.
// It automatically handles:
// - Type assertion and conversion (int, float64→float32, uint64→int)
// - Pointer allocation
// - JSON tag field name mapping
// - Recursive processing of nested structs
func MapToStruct(data map[string]interface{}, obj interface{}) error {
	if data == nil {
		return nil
	}

	v := reflect.ValueOf(obj)

	// obj must be a pointer (e.g., &protocol.CPUConfig{})
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("MapToStruct requires non-nil pointer to struct")
	}

	v = v.Elem()

	if v.Kind() != reflect.Struct {
		return fmt.Errorf("MapToStruct requires pointer to struct")
	}

	t := v.Type()

	// Iterate over struct fields
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		// Get map key from JSON tag
		jsonTag := fieldType.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		parts := strings.Split(jsonTag, ",")
		fieldName := parts[0]

		// Read value from map
		mapValue, exists := data[fieldName]
		if !exists {
			continue
		}

		// Set field value according to type
		if err := setFieldValue(field, mapValue); err != nil {
			// Ignore type mismatch fields (forward compatibility)
			continue
		}
	}

	return nil
}

// setFieldValue handles field assignment, including type conversion and pointer allocation.
func setFieldValue(field reflect.Value, mapValue interface{}) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}

	fieldType := field.Type()

	// Handle pointer type fields (all Protocol struct fields are pointers)
	if fieldType.Kind() == reflect.Ptr {
		elemType := fieldType.Elem()

		// Create new pointer value
		newValue := reflect.New(elemType)

		// Convert according to target type
		switch elemType.Kind() {
		case reflect.Int:
			// Handle int type (source may be int, uint64, float64)
			switch v := mapValue.(type) {
			case int:
				newValue.Elem().SetInt(int64(v))
			case uint64:
				newValue.Elem().SetInt(int64(v))
			case int64:
				newValue.Elem().SetInt(v)
			case float64:
				newValue.Elem().SetInt(int64(v))
			default:
				return fmt.Errorf("cannot convert %T to int", mapValue)
			}

		case reflect.Float32:
			// Handle float32 (source is usually float64)
			switch v := mapValue.(type) {
			case float64:
				newValue.Elem().SetFloat(v)
			case float32:
				newValue.Elem().SetFloat(float64(v))
			default:
				return fmt.Errorf("cannot convert %T to float32", mapValue)
			}

		case reflect.String:
			if str, ok := mapValue.(string); ok {
				newValue.Elem().SetString(str)
			} else {
				return fmt.Errorf("cannot convert %T to string", mapValue)
			}

		case reflect.Struct:
			// Nested struct (e.g., CacheConfig)
			if nestedMap, ok := mapValue.(map[string]interface{}); ok {
				// Recursive processing
				if err := MapToStruct(nestedMap, newValue.Interface()); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("cannot convert %T to struct", mapValue)
			}

		default:
			return fmt.Errorf("unsupported field type: %s", elemType.Kind())
		}

		// Assign the newly created pointer to the field
		field.Set(newValue)

	} else {
		// Non-pointer field (rare in Protocol)
		// Direct assignment
		field.Set(reflect.ValueOf(mapValue))
	}

	return nil
}

// MergeMaps merges two maps, with statsMap values overriding configMap values for duplicate keys.
// This is used to merge configuration parameters (from configRef) with runtime statistics.
func MergeMaps(configMap, statsMap map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(configMap)+len(statsMap))

	// First copy configuration parameters
	for k, v := range configMap {
		merged[k] = v
	}

	// Statistics data overrides (higher priority)
	for k, v := range statsMap {
		merged[k] = v
	}

	return merged
}
