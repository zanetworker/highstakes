package heatmap

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

// TestSchema_CoversExportedFields keeps schema.json in step with the JSON
// tags of the heatmap types, so a new field cannot ship undocumented.
func TestSchema_CoversExportedFields(t *testing.T) {
	raw, err := os.ReadFile("../../schema.json")
	if err != nil {
		t.Fatal(err)
	}
	type node struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	var schema struct {
		Properties  map[string]json.RawMessage `json:"properties"`
		Definitions map[string]node            `json:"definitions"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema.json: %v", err)
	}
	var metadata node
	if err := json.Unmarshal(schema.Properties["metadata"], &metadata); err != nil {
		t.Fatalf("schema.json metadata: %v", err)
	}
	tests := []struct {
		name  string
		typ   reflect.Type
		props map[string]json.RawMessage
	}{
		{"metadata", reflect.TypeOf(types.Metadata{}), metadata.Properties},
		{"FileHeat", reflect.TypeOf(types.FileHeat{}), schema.Definitions["FileHeat"].Properties},
		{"Unit", reflect.TypeOf(types.Unit{}), schema.Definitions["Unit"].Properties},
		{"Placement", reflect.TypeOf(types.Placement{}), schema.Definitions["Placement"].Properties},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.props) == 0 {
				t.Fatalf("schema.json has no properties for %s", tt.name)
			}
			for i := 0; i < tt.typ.NumField(); i++ {
				tag := strings.Split(tt.typ.Field(i).Tag.Get("json"), ",")[0]
				if tag == "" || tag == "-" {
					continue
				}
				if _, ok := tt.props[tag]; !ok {
					t.Errorf("schema.json %s is missing %q (field %s)", tt.name, tag, tt.typ.Field(i).Name)
				}
			}
		})
	}
}
