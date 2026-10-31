package llm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestParseAssessment_TruncatedJSON(t *testing.T) {
	// This is the exact pattern from DeepSeek truncation
	input := `{"security_impact":95,"data_impact":80,"availability_impact":85,"user_impact":90,"blast_radius_summary":"Breaks all network access decisions, bypasses sandbox isolation","critical_reason":"Network pol`

	a, err := parseAssessment(input)
	if err != nil {
		t.Fatalf("should repair truncated JSON, got error: %v", err)
	}

	if a.SecurityImpact != 95 {
		t.Errorf("security: got %d, want 95", a.SecurityImpact)
	}
	if a.AvailabilityImpact != 85 {
		t.Errorf("availability: got %d, want 85", a.AvailabilityImpact)
	}
}

func TestParseAssessment_TruncatedMidNumber(t *testing.T) {
	input := `{"security_impact":85,"data_impact":30,"availability_impact":90,"user_impact":75,"blast_radius_summary":"Orphaned VM subprocesses leak","critical`

	a, err := parseAssessment(input)
	if err != nil {
		t.Fatalf("should repair truncated JSON, got error: %v", err)
	}

	if a.SecurityImpact != 85 {
		t.Errorf("security: got %d, want 85", a.SecurityImpact)
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
		{"config.yaml", true},
		{".github/workflows/ci.yml", true},
		{"charts/app/values.yaml", true},
		{"pyproject.toml", true},
		{"tsconfig.json", true},
		{"Dockerfile", true},
		{"infra/main.tf", true},
		{"package-lock.json", false},
		{"pnpm-lock.yaml", false},
		{"Cargo.lock", false},
		{"poetry.lock", false},
		{"deploy/zz_generated.deepcopy.yaml", false},
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
	_ = os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0644)

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

func TestAssessFile_RetryFailures(t *testing.T) {
	okBody := `{"choices":[{"message":{"content":"{\"security_impact\":10,\"data_impact\":0,\"availability_impact\":0,\"user_impact\":0,\"blast_radius_summary\":\"s\",\"critical_reason\":\"r\"}"}}]}`

	tests := []struct {
		name      string
		server    func(calls *int32) *httptest.Server
		closed    bool
		wantErr   string
		wantCalls int32
	}{
		{
			name: "network failure on every attempt returns error",
			server: func(calls *int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			},
			closed:  true,
			wantErr: "API request failed after 3 attempts",
		},
		{
			name: "429 on every attempt returns rate limit error",
			server: func(calls *int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					w.WriteHeader(http.StatusTooManyRequests)
				}))
			},
			wantErr:   "rate limited (429) after 3 attempts",
			wantCalls: 3,
		},
		{
			name: "429 then success recovers",
			server: func(calls *int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if atomic.AddInt32(calls, 1) < 3 {
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					_, _ = w.Write([]byte(okBody))
				}))
			},
			wantCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			srv := tt.server(&calls)
			if tt.closed {
				srv.Close()
			} else {
				defer srv.Close()
			}
			a := &Assessor{apiURL: srv.URL, model: "m", client: &http.Client{Timeout: time.Second}, retryDelay: time.Millisecond}

			got, err := a.assessFile(FileInput{Path: "x.go", Content: "package x"})

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if got.SecurityImpact != 10 {
					t.Errorf("SecurityImpact = %d, want 10", got.SecurityImpact)
				}
			}
			if !tt.closed && atomic.LoadInt32(&calls) != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestAssessFiles_CacheKeyedByModel(t *testing.T) {
	okBody := `{"choices":[{"message":{"content":"{\"security_impact\":10,\"data_impact\":0,\"availability_impact\":0,\"user_impact\":0,\"blast_radius_summary\":\"s\",\"critical_reason\":\"r\"}"}}]}`
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	cacheDir := t.TempDir()
	files := []FileInput{{Path: "x.go", Content: "package x"}}

	newAssessor := func(model string) *Assessor {
		return &Assessor{apiURL: srv.URL, model: model, cacheDir: cacheDir, concurrency: 1,
			client: &http.Client{Timeout: time.Second}, retryDelay: time.Millisecond}
	}

	steps := []struct {
		name      string
		model     string
		wantCalls int32
	}{
		{"first run with model A calls the API", "model-a", 1},
		{"same content and model A hits the cache", "model-a", 0},
		{"same content with model B misses the cache", "model-b", 1},
		{"model B is now cached too", "model-b", 0},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			atomic.StoreInt32(&calls, 0)
			res, err := newAssessor(st.model).AssessFiles(files)
			if err != nil {
				t.Fatal(err)
			}
			if res["x.go"] == nil {
				t.Fatal("missing assessment for x.go")
			}
			if got := atomic.LoadInt32(&calls); got != st.wantCalls {
				t.Errorf("API calls = %d, want %d", got, st.wantCalls)
			}
		})
	}
}
