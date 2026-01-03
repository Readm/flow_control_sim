package protocol

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// OpenAPISpec represents the minimal structure we need to parse from openapi.yaml
type OpenAPISpec struct {
	Components struct {
		Schemas map[string]Schema `yaml:"schemas"`
	} `yaml:"components"`
}

type Schema struct {
	Type       string              `yaml:"type"`
	Properties map[string]Property `yaml:"properties"`
	Required   []string            `yaml:"required"`
}

type Property struct {
	Type string `yaml:"type"`
	// We can check sub-properties if needed, but for now we focus on top-level fields
}

func TestOpenAPIConsistency(t *testing.T) {
	// 1. Locate openapi.yaml
	// Assuming test is run from project root or tests/ dir.
	// We try to find project root first.
	wd, _ := os.Getwd()
	root := findProjectRoot(wd)
	yamlPath := filepath.Join(root, "web", "openapi.yaml")

	// 2. Read and Parse YAML
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("Failed to read openapi.yaml at %s: %v", yamlPath, err)
	}

	var spec OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("Failed to parse openapi.yaml: %v", err)
	}

	// 3. Define mappings to check
	// Go Struct -> OpenAPI Schema Name
	checks := []struct {
		GoType     interface{}
		SchemaName string
	}{
		{CyNetwork{}, "Network"},
		{CyNode{}, "Node"},
		{CyEdge{}, "Edge"},
	}

	for _, check := range checks {
		t.Run("Check_"+check.SchemaName, func(t *testing.T) {
			schema, ok := spec.Components.Schemas[check.SchemaName]
			if !ok {
				t.Fatalf("Schema %s not found in openapi.yaml", check.SchemaName)
			}
			validateStruct(t, check.GoType, schema)
		})
	}
}

func validateStruct(t *testing.T, s interface{}, schema Schema) {
	val := reflect.TypeOf(s)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// Gather fields from Go struct JSON tags
	goFields := make(map[string]bool)
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		// tag might be "name,omitempty"
		parts := strings.Split(tag, ",")
		name := parts[0]
		goFields[name] = true
	}

	// Check that all required properties in Schema exist in Go fields
	for _, req := range schema.Required {
		if !goFields[req] {
			t.Errorf("Missing required field '%s' in Go struct %s", req, val.Name())
		}
	}

	// Optional: Check that all Go fields exist in Schema (strict mode)
	// We might allow Go to have MORE fields (hidden ones), but usually we want 1:1.
	// For now, let's just warn or check required ones.
	for propName := range schema.Properties {
		if !goFields[propName] {
			// Some properties might be implicit or composed, but let's report mismatch
			t.Logf("Warning: Schema property '%s' not explicitly found in Go struct %s (might be embedded or named differently)", propName, val.Name())
		}
	}
}

func findProjectRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start // fallback
		}
		current = parent
	}
}
