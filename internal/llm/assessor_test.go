package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAssessment_CleanJSON(t *testing.T) {
	input := `{"security_impact": 85, "data_impact": 60, "availability_impact": 40, "user_impact": 70, "blast_radius_summary": "Auth boundary", "critical_reason": "OIDC tokens"}`

	a, err := parseAssessment(input)
	if err != nil {
		t.Fatal(err)
	}

	if a.SecurityImpact != 85 {
		t.Errorf("security: got %d, want 85", a.SecurityImpact)
	}
	if a.DataImpact != 60 {
		t.Errorf("data: got %d, want 60", a.DataImpact)
	}
	if a.BlastRadiusSummary != "Auth boundary" {
		t.Errorf("summary: got %q", a.BlastRadiusSummary)
	}
}

func TestParseAssessment_WrappedInMarkdown(t *testing.T) {
	input := "Here is the assessment:\n```json\n{\"security_impact\": 90, \"data_impact\": 50, \"availability_impact\": 30, \"user_impact\": 80, \"blast_radius_summary\": \"test\", \"critical_reason\": \"\"}\n```"

	a, err := parseAssessment(input)
	if err != nil {
		t.Fatal(err)
	}

	if a.SecurityImpact != 90 {
		t.Errorf("security: got %d, want 90", a.SecurityImpact)
	}
}

func TestParseAssessment_InvalidJSON(t *testing.T) {
	_, err := parseAssessment("this is not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseAssessment_ClampsValues(t *testing.T) {
	input := `{"security_impact": 150, "data_impact": -10, "availability_impact": 50, "user_impact": 200, "blast_radius_summary": "", "critical_reason": ""}`

	a, err := parseAssessment(input)
	if err != nil {
		t.Fatal(err)
	}

	if a.SecurityImpact != 100 {
		t.Errorf("security should clamp to 100, got %d", a.SecurityImpact)
	}
	if a.DataImpact != 0 {
		t.Errorf("data should clamp to 0, got %d", a.DataImpact)
	}
}

func TestMaxScore(t *testing.T) {
	tests := []struct {
		name     string
		a        Assessment
		expected int
	}{
		{"security highest", Assessment{SecurityImpact: 90, DataImpact: 50, AvailabilityImpact: 30, UserImpact: 70}, 90},
		{"data highest", Assessment{SecurityImpact: 10, DataImpact: 80, AvailabilityImpact: 30, UserImpact: 70}, 80},
		{"all zero", Assessment{}, 0},
		{"all equal", Assessment{SecurityImpact: 50, DataImpact: 50, AvailabilityImpact: 50, UserImpact: 50}, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.MaxScore(); got != tt.expected {
				t.Errorf("MaxScore() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestShouldAssess(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"src/auth/jwt.go", true},
		{"src/auth/jwt_test.go", false},
		{"src/handler.rs", true},
		{"test_something.py", false},
		{"generated.pb.go", false},
		{"README.md", false},
		{"config.yaml", false},
		{"go.mod", false},
		{"go.sum", false},
		{"src/main.rs", true},
		{"app.py", true},
		{"handler.test.ts", false},
		{"handler.spec.js", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ShouldAssess(tt.path); got != tt.expected {
				t.Errorf("ShouldAssess(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestContentHash_Deterministic(t *testing.T) {
	content := "package main\nfunc main() {}\n"
	h1 := contentHash(content)
	h2 := contentHash(content)

	if h1 != h2 {
		t.Errorf("same content should produce same hash: %s vs %s", h1, h2)
	}
}

func TestContentHash_Different(t *testing.T) {
	h1 := contentHash("package main")
	h2 := contentHash("package other")

	if h1 == h2 {
		t.Error("different content should produce different hashes")
	}
}

func TestCache_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	a := &Assessor{cacheDir: dir}

	original := &Assessment{
		SecurityImpact:     85,
		DataImpact:         60,
		BlastRadiusSummary: "Auth boundary",
		ContentHash:        "abc123",
	}

	a.saveCache("abc123", original)

	loaded, ok := a.loadCache("abc123")
	if !ok {
		t.Fatal("cache miss after save")
	}

	if loaded.SecurityImpact != 85 {
		t.Errorf("security: got %d, want 85", loaded.SecurityImpact)
	}
	if loaded.BlastRadiusSummary != "Auth boundary" {
		t.Errorf("summary: got %q", loaded.BlastRadiusSummary)
	}
}

func TestCache_Miss(t *testing.T) {
	dir := t.TempDir()
	a := &Assessor{cacheDir: dir}

	_, ok := a.loadCache("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCache_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	a := &Assessor{cacheDir: dir}

	// Write corrupt cache file
	os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0644)

	_, ok := a.loadCache("corrupt")
	if ok {
		t.Error("corrupted cache should return miss")
	}
}

func TestBuildPrompt_ContainsFileContent(t *testing.T) {
	prompt := buildPrompt("src/auth.rs", "fn validate_token() {}")

	if len(prompt) == 0 {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "src/auth.rs") {
		t.Error("prompt should contain file path")
	}
	if !contains(prompt, "validate_token") {
		t.Error("prompt should contain file content")
	}
	if !contains(prompt, "security_impact") {
		t.Error("prompt should request structured output")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
