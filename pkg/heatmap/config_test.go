package heatmap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Owners(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want map[string]string
	}{
		{"owners alias map is read", "version: 1.0.0\nowners:\n  alice@x.com: \"@alice\"\n  bob@x.com: bob-gh\n",
			map[string]string{"alice@x.com": "@alice", "bob@x.com": "bob-gh"}},
		{"missing owners is nil", "version: 1.0.0\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Owners) != len(tt.want) {
				t.Fatalf("owners = %v, want %v", cfg.Owners, tt.want)
			}
			for k, v := range tt.want {
				if cfg.Owners[k] != v {
					t.Errorf("owners[%s] = %q, want %q", k, cfg.Owners[k], v)
				}
			}
		})
	}

	t.Run("owners round-trip through SaveConfig", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg := DefaultConfig()
		cfg.Owners = map[string]string{"a@x.com": "@a"}
		if err := SaveConfig(cfg, path); err != nil {
			t.Fatal(err)
		}
		got, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Owners["a@x.com"] != "@a" {
			t.Errorf("owners after round trip = %v", got.Owners)
		}
	})
}
