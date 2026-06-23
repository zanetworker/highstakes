package heatmap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		substrings []string
		expected   bool
	}{
		{"exact match in dir", "src/auth/jwt.go", []string{"auth"}, true},
		{"match in filename", "src/api/auth_handler.go", []string{"auth"}, true},
		{"no match", "src/utils/helpers.go", []string{"auth", "payment"}, false},
		{"match substring", "src/api/user_controller.go", []string{"controller"}, true},
		{"match with slash", "src/api/routes.go", []string{"api/"}, true},
		{"case insensitive", "src/Auth/JWT.go", []string{"auth"}, true},
		{"empty substrings", "src/main.go", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAny(tt.path, tt.substrings)
			if result != tt.expected {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.path, tt.substrings, result, tt.expected)
			}
		})
	}
}

func TestDetectUserImpact(t *testing.T) {
	tests := []struct {
		path     string
		wantUser bool
		wantAuth bool
		wantData bool
		wantPay  bool
	}{
		{"src/auth/jwt.go", false, true, false, false},
		{"src/api/handler.go", true, false, false, false},
		{"src/api/auth_handler.go", true, true, false, false},
		{"src/db/schema.go", false, false, true, false},
		{"src/payment/checkout.go", false, false, false, true},
		{"src/utils/format.go", false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			user, auth, data, pay := detectUserImpact(tt.path)
			if user != tt.wantUser {
				t.Errorf("userFacing: got %v, want %v", user, tt.wantUser)
			}
			if auth != tt.wantAuth {
				t.Errorf("affectsAuth: got %v, want %v", auth, tt.wantAuth)
			}
			if data != tt.wantData {
				t.Errorf("affectsData: got %v, want %v", data, tt.wantData)
			}
			if pay != tt.wantPay {
				t.Errorf("affectsPayments: got %v, want %v", pay, tt.wantPay)
			}
		})
	}
}

func TestDetectDataSensitivity(t *testing.T) {
	tests := []struct {
		path          string
		wantPII       bool
		wantSecrets   bool
		wantFinancial bool
	}{
		{"src/user/profile.go", true, false, false},
		{"src/auth/token.go", false, true, false},
		{"src/billing/invoice.go", false, false, true},
		{"src/utils/format.go", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			pii, secrets, financial := detectDataSensitivity(tt.path)
			if pii != tt.wantPII {
				t.Errorf("handlesPII: got %v, want %v", pii, tt.wantPII)
			}
			if secrets != tt.wantSecrets {
				t.Errorf("handlesSecrets: got %v, want %v", secrets, tt.wantSecrets)
			}
			if financial != tt.wantFinancial {
				t.Errorf("handlesFinancial: got %v, want %v", financial, tt.wantFinancial)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heatmap.json")

	original := &types.Heatmap{
		Version: "1.0.0",
		Metadata: types.Metadata{
			RepoPath:   "/test/repo",
			TotalFiles: 5,
			Languages:  map[string]int{"Go": 3, "Python": 2},
		},
		Files: map[string]*types.FileHeat{
			"main.go": {
				Path:      "main.go",
				HeatScore: 75,
				Tier:      types.TierHigh,
				Language:  "Go",
			},
		},
		Incidents:   []types.Incident{},
		Annotations: map[string]types.Annotation{},
	}

	// Save
	if err := Save(original, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("heatmap file was not created")
	}

	// Load
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify round-trip
	if loaded.Version != original.Version {
		t.Errorf("version: got %q, want %q", loaded.Version, original.Version)
	}
	if loaded.Metadata.TotalFiles != original.Metadata.TotalFiles {
		t.Errorf("total files: got %d, want %d", loaded.Metadata.TotalFiles, original.Metadata.TotalFiles)
	}
	if len(loaded.Files) != len(original.Files) {
		t.Errorf("files count: got %d, want %d", len(loaded.Files), len(original.Files))
	}
	if loaded.Files["main.go"].HeatScore != 75 {
		t.Errorf("heat score: got %d, want 75", loaded.Files["main.go"].HeatScore)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/heatmap.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSave_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heatmap.json")

	hm := &types.Heatmap{
		Version: "1.0.0",
		Files:   map[string]*types.FileHeat{},
	}

	if err := Save(hm, path); err != nil {
		t.Fatal(err)
	}

	// Read and verify it's valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("saved file is not valid JSON: %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", config.Version)
	}
	if config.Tiers.Critical != 86 {
		t.Errorf("expected critical threshold 86, got %d", config.Tiers.Critical)
	}
	if config.CircuitBreakers.MaxDiffLines != 500 {
		t.Errorf("expected max diff lines 500, got %d", config.CircuitBreakers.MaxDiffLines)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := DefaultConfig()
	if err := SaveConfig(original, path); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Tiers.Critical != original.Tiers.Critical {
		t.Errorf("critical threshold: got %d, want %d", loaded.Tiers.Critical, original.Tiers.Critical)
	}
}
