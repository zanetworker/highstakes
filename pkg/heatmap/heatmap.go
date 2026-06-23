package heatmap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zanetworker/highstakes/internal/analyzer"
	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/llm"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

const Version = "1.0.0"

// GenerateOptions controls analysis behavior
type GenerateOptions struct {
	NoLLM       bool
	ForceReassess bool
	Model       string
	Concurrency int
}

// Generator generates heatmaps for a repository
type Generator struct {
	repoPath string
	config   types.Config
	opts     GenerateOptions
}

// New creates a new heatmap generator
func New(repoPath string, opts ...GenerateOptions) (*Generator, error) {
	config, err := LoadConfig(filepath.Join(repoPath, ".heatmap", "config.yaml"))
	if err != nil {
		config = DefaultConfig()
	}

	var o GenerateOptions
	if len(opts) > 0 {
		o = opts[0]
	}

	return &Generator{
		repoPath: repoPath,
		config:   config,
		opts:     o,
	}, nil
}

// Generate performs full analysis and generates heatmap
func (g *Generator) Generate() (*types.Heatmap, error) {
	fmt.Println("📊 Analyzing repository...")

	// Initialize analyzers
	staticAnalyzer := analyzer.New(g.repoPath, g.config.Exclude.Dirs, g.config.Exclude.Patterns)
	gitAnalyzer, err := gitanalyzer.New(g.repoPath)
	if err != nil {
		return nil, fmt.Errorf("initialize git analyzer: %w", err)
	}

	// Run static analysis
	fmt.Println("  - Scanning files...")
	if err := staticAnalyzer.Analyze(); err != nil {
		return nil, fmt.Errorf("static analysis: %w", err)
	}

	// Get all files
	allFiles := staticAnalyzer.GetAllFiles()
	filePaths := make([]string, 0, len(allFiles))
	for path := range allFiles {
		filePaths = append(filePaths, path)
	}

	// Run git analysis
	fmt.Println("  - Analyzing git history...")
	gitHistory, err := gitAnalyzer.AnalyzeAll(filePaths, 365)
	if err != nil {
		return nil, fmt.Errorf("git analysis: %w", err)
	}

	// Get git metadata
	branch, _ := gitAnalyzer.GetCurrentBranch()
	commitSHA, _ := gitAnalyzer.GetHeadCommit()

	// Run LLM analysis if enabled
	var assessments map[string]*llm.Assessment
	if !g.opts.NoLLM {
		apiKey := os.Getenv("OPENROUTER_API_KEY")
		if apiKey != "" {
			fmt.Println("  - Assessing blast radius (LLM)...")
			assessor, err := llm.NewAssessor(llm.Config{
				APIKey:      apiKey,
				Model:       g.opts.Model,
				CacheDir:    filepath.Join(g.repoPath, ".heatmap", "cache"),
				Concurrency: g.opts.Concurrency,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: LLM init failed: %v (falling back to static)\n", err)
			} else {
				var inputs []llm.FileInput
				for path := range allFiles {
					if !llm.ShouldAssess(path) {
						continue
					}
					content, err := os.ReadFile(filepath.Join(g.repoPath, path))
					if err != nil {
						continue
					}
					inputs = append(inputs, llm.FileInput{Path: path, Content: string(content)})
				}
				assessments, err = assessor.AssessFiles(inputs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: LLM assessment failed: %v\n", err)
				}
			}
		} else {
			fmt.Println("  - Skipping LLM analysis (OPENROUTER_API_KEY not set)")
		}
	}

	// Build heatmap
	fmt.Println("  - Calculating heat scores...")
	heatmap := &types.Heatmap{
		Version: Version,
		Metadata: types.Metadata{
			RepoPath:   g.repoPath,
			AnalyzedAt: time.Now(),
			CommitSHA:  commitSHA,
			Branch:     branch,
			TotalFiles: len(allFiles),
			Languages:  countLanguages(allFiles),
		},
		Files:       make(map[string]*types.FileHeat),
		Incidents:   []types.Incident{},
		Annotations: make(map[string]types.Annotation),
	}

	// Process each file
	for path, fileInfo := range allFiles {
		heat := g.processFile(path, fileInfo, staticAnalyzer, gitHistory)

		// Apply LLM assessment if available
		if assessment, ok := assessments[path]; ok {
			heat.Factors.BlastRadius = types.BlastRadius{
				Score:              assessment.MaxScore(),
				SecurityImpact:     assessment.SecurityImpact,
				DataImpact:         assessment.DataImpact,
				AvailabilityImpact: assessment.AvailabilityImpact,
				UserImpact:         assessment.UserImpact,
				Summary:            assessment.BlastRadiusSummary,
				CriticalReason:     assessment.CriticalReason,
				Assessed:           true,
			}
			// Recalculate heat score with LLM data
			heat.HeatScore = scorer.CalculateHeatScore(heat.Factors)
			heat.Tier = scorer.CalculateTier(heat.HeatScore, g.config.Tiers)
			heat.ReviewRequirements = scorer.CalculateReviewRequirements(heat.Tier)
		}

		heatmap.Files[path] = heat
	}

	// Load existing incidents if any
	incidents, err := LoadIncidents(filepath.Join(g.repoPath, ".heatmap", "incidents.json"))
	if err == nil {
		heatmap.Incidents = incidents
	}

	// Load existing annotations if any
	annotations, err := LoadAnnotations(filepath.Join(g.repoPath, ".heatmap", "annotations.json"))
	if err == nil {
		heatmap.Annotations = annotations
	}

	return heatmap, nil
}

// processFile combines static and git analysis into FileHeat
func (g *Generator) processFile(path string, fileInfo *analyzer.FileInfo, staticAnalyzer *analyzer.Analyzer, gitHistory map[string]*gitanalyzer.FileHistory) *types.FileHeat {
	// Build factors
	factors := types.Factors{}

	// Dependency centrality
	factors.DependencyCentrality = staticAnalyzer.CalculateDependencyCentrality(path)

	// Change frequency
	if hist, ok := gitHistory[path]; ok {
		factors.ChangeFrequency = types.ChangeFrequency{
			Score:          scorer.CalculateChangeFrequencyScore(hist.CommitsLast90d),
			CommitCount:    hist.CommitCount,
			CommitsLast90d: hist.CommitsLast90d,
			UniqueAuthors:  len(hist.UniqueAuthors),
		}
	} else {
		factors.ChangeFrequency = types.ChangeFrequency{Score: 0}
	}

	// Incident history (load from annotations)
	incidents := g.getFileIncidents(path)
	factors.IncidentHistory = types.IncidentHistory{
		Score:             scorer.CalculateIncidentScore(incidents, time.Now()),
		IncidentCount:     len(incidents),
		SeverityBreakdown: countSeverities(incidents),
	}
	if len(incidents) > 0 {
		lastIncident := incidents[len(incidents)-1].Date
		factors.IncidentHistory.LastIncident = &lastIncident
	}

	// User impact (heuristics based on file path)
	userFacing, affectsAuth, affectsData, affectsPayments := detectUserImpact(path)
	factors.UserImpact = types.UserImpact{
		Score:           scorer.CalculateUserImpactScore(userFacing, affectsAuth, affectsData, affectsPayments),
		UserFacing:      userFacing,
		AffectsAuth:     affectsAuth,
		AffectsData:     affectsData,
		AffectsPayments: affectsPayments,
	}

	// Data sensitivity (heuristics)
	pii, secrets, financial := detectDataSensitivity(path)
	factors.DataSensitivity = types.DataSensitivity{
		Score:            scorer.CalculateDataSensitivityScore(pii, secrets, financial),
		HandlesPII:       pii,
		HandlesSecrets:   secrets,
		HandlesFinancial: financial,
	}

	// Test coverage (placeholder - would integrate with coverage tools)
	factors.TestCoverage = types.TestCoverage{
		Score:           50, // Placeholder
		CoveragePercent: 50.0,
		TestCount:       0,
		HasIntegrationTests: false,
	}

	// Complexity
	factors.Complexity = types.Complexity{
		Score:         scorer.CalculateComplexityScore(fileInfo.Cyclomatic, fileInfo.Cognitive),
		Cyclomatic:    fileInfo.Cyclomatic,
		Cognitive:     fileInfo.Cognitive,
		FunctionCount: fileInfo.FunctionCount,
	}

	// Calculate aggregate heat score
	heatScore := scorer.CalculateHeatScore(factors)
	tier := scorer.CalculateTier(heatScore, g.config.Tiers)

	// Build dependencies
	deps := types.Dependencies{
		ImportedBy: make([]types.Dependency, 0, len(fileInfo.ImportedBy)),
		Imports:    make([]types.Dependency, 0, len(fileInfo.Imports)),
	}

	for _, imp := range fileInfo.ImportedBy {
		deps.ImportedBy = append(deps.ImportedBy, types.Dependency{
			Path:      imp,
			HeatScore: 0, // Will be filled in second pass if needed
		})
	}

	// Recent changes
	var recentChanges []types.Change
	if hist, ok := gitHistory[path]; ok {
		recentChanges = hist.RecentChanges
	}

	return &types.FileHeat{
		Path:               path,
		HeatScore:          heatScore,
		Tier:               tier,
		Language:           fileInfo.Language,
		Size:               types.FileSize{Lines: fileInfo.Lines, Bytes: fileInfo.Bytes},
		Factors:            factors,
		Dependencies:       deps,
		RecentChanges:      recentChanges,
		ReviewRequirements: scorer.CalculateReviewRequirements(tier),
	}
}

// getFileIncidents returns incidents for a file
func (g *Generator) getFileIncidents(path string) []types.Incident {
	// Load from .heatmap/incidents.json
	incidents, err := LoadIncidents(filepath.Join(g.repoPath, ".heatmap", "incidents.json"))
	if err != nil {
		return []types.Incident{}
	}

	// Filter by file
	var fileIncidents []types.Incident
	for _, inc := range incidents {
		if inc.File == path {
			fileIncidents = append(fileIncidents, inc)
		}
	}

	return fileIncidents
}

// Save writes heatmap to disk
func Save(h *types.Heatmap, path string) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Load reads heatmap from disk
func Load(path string) (*types.Heatmap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var heatmap types.Heatmap
	if err := json.Unmarshal(data, &heatmap); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}

	return &heatmap, nil
}

// Helper functions

func countLanguages(files map[string]*analyzer.FileInfo) map[string]int {
	counts := make(map[string]int)
	for _, f := range files {
		counts[f.Language]++
	}
	return counts
}

func countSeverities(incidents []types.Incident) map[string]int {
	counts := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}

	for _, inc := range incidents {
		counts[inc.Severity]++
	}

	return counts
}

func detectUserImpact(path string) (userFacing, affectsAuth, affectsData, affectsPayments bool) {
	lower := filepath.ToSlash(path)

	// User-facing: API, handlers, routes, controllers
	userFacing = containsAny(lower, []string{"api/", "handler", "route", "controller", "endpoint"})

	// Auth: authentication, authorization, jwt, session
	affectsAuth = containsAny(lower, []string{"auth", "jwt", "session", "token", "login", "permission"})

	// Data: database, model, schema, migration
	affectsData = containsAny(lower, []string{"db/", "database", "model", "schema", "migration", "sql"})

	// Payments: payment, billing, checkout, stripe
	affectsPayments = containsAny(lower, []string{"payment", "billing", "checkout", "stripe", "paypal", "transaction"})

	return
}

func detectDataSensitivity(path string) (pii, secrets, financial bool) {
	lower := filepath.ToSlash(path)

	// PII: user data, profile, personal
	pii = containsAny(lower, []string{"user", "profile", "personal", "customer", "account"})

	// Secrets: auth, config with secrets, crypto
	secrets = containsAny(lower, []string{"auth", "secret", "crypto", "key", "token", "credential"})

	// Financial: payment, billing, pricing
	financial = containsAny(lower, []string{"payment", "billing", "price", "invoice", "transaction"})

	return
}

func containsAny(s string, substrings []string) bool {
	lower := strings.ToLower(s)
	for _, substr := range substrings {
		if strings.Contains(lower, substr) {
			return true
		}
	}
	return false
}
