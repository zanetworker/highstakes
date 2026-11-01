package analyzer

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]any
	}{
		{"plain json is unchanged", `{"a": 1}`, map[string]any{"a": 1.0}},
		{"trailing comma before brace", "{\"a\": 1,\n}", map[string]any{"a": 1.0}},
		{"trailing comma in array", `{"a": [1, 2,]}`, map[string]any{"a": []any{1.0, 2.0}}},
		{"line comment after trailing comma", "{\"a\": 1, // note\n}", map[string]any{"a": 1.0}},
		{"block comment after trailing comma", "{\"a\": 1, /* note */ }", map[string]any{"a": 1.0}},
		{"multi-line block comment after trailing comma", "{\"a\": [1, /* x\n y */\n]}", map[string]any{"a": []any{1.0}}},
		{"line comment between members", "{\"a\": 1, // note\n \"b\": 2}", map[string]any{"a": 1.0, "b": 2.0}},
		{"comment-like text inside strings is kept", `{"url": "http://x/*y*/", "p": "a // b",}`, map[string]any{"url": "http://x/*y*/", "p": "a // b"}},
		{"escaped quote inside string", `{"q": "say \"//hi\"",}`, map[string]any{"q": `say "//hi"`}},
		{"comma inside string before brace is kept", `{"s": ",}"}`, map[string]any{"s": ",}"}},
		{"unterminated block comment", "{\"a\": 1} /* open", map[string]any{"a": 1.0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := stripJSONC([]byte(tt.in))
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("stripJSONC(%q) = %q is not valid JSON: %v", tt.in, out, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stripJSONC(%q) parsed to %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
