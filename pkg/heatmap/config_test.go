package heatmap

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadConfig_AutonomyMaxLevel(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    int
		wantErr bool
	}{
		{"unset is 3", "version: 1.0.0\n", 3, false},
		{"set", "autonomy:\n  max_level: 1\n", 1, false},
		{"zero is a valid cap", "autonomy:\n  max_level: 0\n", 0, false},
		{"out of range is rejected", "autonomy:\n  max_level: 4\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got, _ := cfg.Autonomy.Max(); got != tt.want {
				t.Errorf("max = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNew_Config(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string // "" leaves the config file absent
		wantErr string
	}{
		{"absent config uses defaults", "", ""},
		{"valid config", "autonomy:\n  max_level: 2\n", ""},
		{"invalid max_level is rejected", "autonomy:\n  max_level: 7\n", "Valid values: 0, 1, 2, 3"},
		{"unparseable config is rejected", "autonomy: [\n", ".heatmap/config.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.yaml != "" {
				if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".heatmap", "config.yaml"), []byte(tt.yaml), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := New(dir)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("New err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
