package heatmap

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/analyzer"
	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/scorer"
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

func TestProcessFile_PersistsDependencies(t *testing.T) {
	tests := []struct {
		name           string
		info           *analyzer.FileInfo
		wantImports    []string
		wantImportedBy []string
	}{
		{
			name:           "imports and importers are both persisted",
			info:           &analyzer.FileInfo{Path: "app/app.go", Imports: []string{"example.com/m/core"}, DependsOn: []string{"core/core.go"}, ImportedBy: []string{"main.go"}},
			wantImports:    []string{"core/core.go"},
			wantImportedBy: []string{"main.go"},
		},
		{
			name:           "no dependencies yields empty lists",
			info:           &analyzer.FileInfo{Path: "leaf.go"},
			wantImports:    []string{},
			wantImportedBy: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Generator{config: DefaultConfig()}
			heat := g.processFile(tt.info.Path, tt.info, analyzer.New("", nil, nil), nil)
			gotImports := []string{}
			for _, d := range heat.Dependencies.Imports {
				gotImports = append(gotImports, d.Path)
			}
			gotImportedBy := []string{}
			for _, d := range heat.Dependencies.ImportedBy {
				gotImportedBy = append(gotImportedBy, d.Path)
			}
			if !reflect.DeepEqual(gotImports, tt.wantImports) {
				t.Errorf("Imports = %v, want %v", gotImports, tt.wantImports)
			}
			if !reflect.DeepEqual(gotImportedBy, tt.wantImportedBy) {
				t.Errorf("ImportedBy = %v, want %v", gotImportedBy, tt.wantImportedBy)
			}
		})
	}
}

func TestGenerate_GitHistoryStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	run := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=d", "GIT_AUTHOR_EMAIL=d@x.io", "GIT_COMMITTER_NAME=d", "GIT_COMMITTER_EMAIL=d@x.io")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	tests := []struct {
		name        string
		commits     int
		wantStatus  string // prefix
		wantCommits int
	}{
		{"history read for every file", 3, "complete", 3},
		{"no commits: analyze still succeeds, metadata says why", 0, "unavailable", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			run(t, dir, "init", "-q")
			for i := 0; i < tt.commits; i++ {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// v"+strconv.Itoa(i)+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				run(t, dir, "add", "main.go")
				run(t, dir, "commit", "-q", "-m", "c"+strconv.Itoa(i))
			}
			if tt.commits == 0 {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			g, err := New(dir, GenerateOptions{NoLLM: true})
			if err != nil {
				t.Fatal(err)
			}
			hm, err := g.Generate()
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if !strings.HasPrefix(hm.Metadata.GitHistory, tt.wantStatus) {
				t.Errorf("Metadata.GitHistory = %q, want prefix %q", hm.Metadata.GitHistory, tt.wantStatus)
			}
			f, ok := hm.Files["main.go"]
			if !ok {
				t.Fatal("main.go missing from heatmap")
			}
			if got := f.Factors.ChangeFrequency.CommitCount; got != tt.wantCommits {
				t.Errorf("CommitCount = %d, want %d", got, tt.wantCommits)
			}
			if f.Explanation == nil || len(f.Explanation.Actions) == 0 {
				t.Errorf("Explanation = %+v, want headline and actions", f.Explanation)
			}
		})
	}
}

func TestProcessFile_CapsTestFiles(t *testing.T) {
	// High complexity and churn would put both files above MEDIUM; only the
	// test file is capped.
	hot := func(path string) *analyzer.FileInfo {
		return &analyzer.FileInfo{Path: path, Cyclomatic: 40, Cognitive: 60, ImportedBy: []string{"a.go", "b.go"}}
	}
	history := map[string]*gitanalyzer.FileHistory{
		"pay/charge.go":      {CommitCount: 80, CommitsLast90d: 60},
		"pay/charge_test.go": {CommitCount: 80, CommitsLast90d: 60},
	}
	g := &Generator{config: DefaultConfig()}
	medium := g.config.Tiers.Medium

	src := g.processFile("pay/charge.go", hot("pay/charge.go"), analyzer.New("", nil, nil), history)
	test := g.processFile("pay/charge_test.go", hot("pay/charge_test.go"), analyzer.New("", nil, nil), history)

	if src.HeatScore < medium {
		t.Fatalf("fixture too cold: source heat %d < medium %d", src.HeatScore, medium)
	}
	if test.HeatScore != medium-1 || test.Tier != types.TierLow {
		t.Errorf("test file heat=%d tier=%s, want %d low", test.HeatScore, test.Tier, medium-1)
	}
}

func TestProcessFile_Axes(t *testing.T) {
	hot := func(path string) *analyzer.FileInfo {
		return &analyzer.FileInfo{Path: path, Cyclomatic: 40, Cognitive: 60}
	}
	history := map[string]*gitanalyzer.FileHistory{
		"pay/charge.go":      {CommitCount: 80, CommitsLast90d: 60},
		"pay/charge_test.go": {CommitCount: 80, CommitsLast90d: 60},
	}
	g := &Generator{config: DefaultConfig()}
	medium := g.config.Tiers.Medium

	tests := []struct {
		name           string
		path           string
		wantLikelihood int
	}{
		{"source likelihood is the no-LLM score", "pay/charge.go", 50},
		{"test file likelihood is capped", "pay/charge_test.go", medium - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := g.processFile(tt.path, hot(tt.path), analyzer.New("", nil, nil), history)
			if f.Likelihood != tt.wantLikelihood {
				t.Errorf("Likelihood = %d, want %d", f.Likelihood, tt.wantLikelihood)
			}
			if f.ImpactAssessed || f.Quadrant != "" {
				t.Errorf("no LLM: impact_assessed=%v quadrant=%q, want false and empty", f.ImpactAssessed, f.Quadrant)
			}
			if f.HeatScore != f.Likelihood {
				t.Errorf("no LLM: heat %d, want likelihood %d", f.HeatScore, f.Likelihood)
			}
		})
	}
}

func TestScoreFile_Assessed(t *testing.T) {
	th := DefaultConfig().Tiers
	tests := []struct {
		name       string
		isTest     bool
		blast      int
		wantImpact int
		wantHeat   int
	}{
		{"impact is blast radius, heat is the max", false, 90, 90, 90},
		{"low impact: heat is likelihood", false, 10, 10, 58},
		{"test file caps impact", true, 90, th.Medium - 1, th.Medium - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &types.FileHeat{Factors: types.Factors{
				ChangeFrequency: types.ChangeFrequency{Score: 80, CommitCount: 10},
				Complexity:      types.Complexity{Score: 60},
				BlastRadius:     types.BlastRadius{Score: tt.blast, Assessed: true},
			}}
			scoreFile(f, tt.isTest, th)
			if f.Impact != tt.wantImpact || !f.ImpactAssessed || f.HeatScore != tt.wantHeat {
				t.Errorf("impact=%d assessed=%v heat=%d, want %d true %d", f.Impact, f.ImpactAssessed, f.HeatScore, tt.wantImpact, tt.wantHeat)
			}
			if f.Tier != scorer.CalculateTier(f.HeatScore, th) {
				t.Errorf("tier %s not derived from heat %d before the quadrant pass", f.Tier, f.HeatScore)
			}
		})
	}
}

func TestApplyAxes_SetsQuadrantsAndMetadata(t *testing.T) {
	th := DefaultConfig().Tiers
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"a.go": {Path: "a.go", Likelihood: 80, Impact: 70, ImpactAssessed: true, Tier: types.TierLow},
		"b.go": {Path: "b.go", Likelihood: 10, Impact: 10, ImpactAssessed: true, Tier: types.TierHigh},
	}}
	applyAxes(hm, th)
	if hm.Files["a.go"].Quadrant != types.QuadrantFragileCritical || hm.Files["a.go"].Tier != types.TierCritical {
		t.Errorf("a.go = %s/%s, want fragile-critical/critical", hm.Files["a.go"].Quadrant, hm.Files["a.go"].Tier)
	}
	if hm.Files["b.go"].Tier != types.TierLow {
		t.Errorf("b.go tier = %s, want low", hm.Files["b.go"].Tier)
	}
	if hm.Metadata.LikelyCutoff != 80 || hm.Metadata.ImpactThreshold != 70 {
		t.Errorf("metadata cutoff=%d threshold=%d, want 80 70 (top 20%% of impact, above the %d floor)", hm.Metadata.LikelyCutoff, hm.Metadata.ImpactThreshold, th.High)
	}
}

func TestApplyExplanations(t *testing.T) {
	th := DefaultConfig().Tiers
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"a.go":      {Path: "a.go", Likelihood: 80, Impact: 70, ImpactAssessed: true},
		"b.go":      {Path: "b.go", Likelihood: 10, Impact: 10},
		"a_test.go": {Path: "a_test.go", Likelihood: 20, Impact: 10, ImpactAssessed: true},
	}}
	hm.Files["a.go"].Factors.ChangeFrequency.CommitCount = 9
	applyAxes(hm, th)
	applyExplanations(hm, map[string][]ownership.Owner{"a.go": {{Handle: "@ana", Share: 0.9}}})

	tests := []struct {
		path         string
		wantHeadline string
		wantAction   string
	}{
		{"a.go", "FRAGILE AND CRITICAL (impact 70, likelihood 80", "single-owner"},
		{"b.go", "IMPACT NOT ASSESSED", "assess-impact"},
		{"a_test.go", "LOW STAKES", "light-review"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			e := hm.Files[tt.path].Explanation
			if e == nil {
				t.Fatal("no explanation")
			}
			if !strings.HasPrefix(e.Headline, tt.wantHeadline) {
				t.Errorf("headline = %q, want prefix %q", e.Headline, tt.wantHeadline)
			}
			found := false
			for _, a := range e.Actions {
				found = found || a.ID == tt.wantAction
			}
			if !found {
				t.Errorf("actions %v lack %s", e.Actions, tt.wantAction)
			}
		})
	}
}

func TestApplyExplanations_EmptyHeatmap(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	applyExplanations(hm, nil) // must not panic
}

func TestGenerate_ImportsAreRepoPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	tests := []struct {
		name  string
		files map[string]string
		path  string
		want  []string
	}{
		{
			name: "go module import resolves to package files, stdlib dropped",
			files: map[string]string{
				"go.mod":                 "module github.com/acme/app\n\ngo 1.22\n",
				"main.go":                "package main\n\nimport (\n\t\"fmt\"\n\t\"github.com/acme/app/internal/auth\"\n)\n\nfunc main() { fmt.Println(auth.Check()) }\n",
				"internal/auth/auth.go":  "package auth\n\nfunc Check() bool { return true }\n",
				"internal/auth/token.go": "package auth\n\nfunc token() string { return \"\" }\n",
			},
			path: "main.go",
			want: []string{"internal/auth/auth.go", "internal/auth/token.go"},
		},
		{
			name: "python dotted import resolves to module file, third party dropped",
			files: map[string]string{
				"app/main.py": "import requests\nfrom app.auth import check\n",
				"app/auth.py": "def check():\n    return True\n",
			},
			path: "app/main.py",
			want: []string{"app/auth.py"},
		},
		{
			name:  "file with no imports has empty forward graph",
			files: map[string]string{"app/leaf.py": "x = 1\n"},
			path:  "app/leaf.py",
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			for p, body := range tt.files {
				full := filepath.Join(dir, p)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			g, err := New(dir, GenerateOptions{NoLLM: true})
			if err != nil {
				t.Fatal(err)
			}
			hm, err := g.Generate()
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			f, ok := hm.Files[tt.path]
			if !ok {
				t.Fatalf("%s missing from heatmap", tt.path)
			}
			got := []string{}
			for _, d := range f.Dependencies.Imports {
				got = append(got, d.Path)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Imports = %v, want %v", got, tt.want)
			}
			for _, imp := range got {
				target, ok := hm.Files[imp]
				if !ok {
					t.Errorf("import %s is not a heatmap file", imp)
					continue
				}
				found := false
				for _, d := range target.Dependencies.ImportedBy {
					found = found || d.Path == tt.path
				}
				if !found {
					t.Errorf("%s imports %s but %s.ImportedBy lacks it", tt.path, imp, imp)
				}
			}
		})
	}
}

func TestLLMInputs_SkipsSensitiveFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":            "package main\n",
		"credentials.json":   `{"user": "a"}`,
		"deploy/values.yaml": "key: |\n  -----BEGIN PRIVATE KEY-----\n",
		"main_test.go":       "package main\n",
	}
	for p, c := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"main.go", "credentials.json", "deploy/values.yaml", "main_test.go", "missing.go"}

	inputs, sensitive := llmInputs(dir, paths)

	var sent []string
	for _, in := range inputs {
		sent = append(sent, in.Path)
	}
	if !reflect.DeepEqual(sent, []string{"main.go"}) {
		t.Errorf("sent to LLM = %v, want [main.go]", sent)
	}
	wantSensitive := map[string]bool{"credentials.json": true, "deploy/values.yaml": true}
	if !reflect.DeepEqual(sensitive, wantSensitive) {
		t.Errorf("sensitive = %v, want %v", sensitive, wantSensitive)
	}
}
