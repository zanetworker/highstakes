package types

import "time"

// Heatmap represents the complete heatmap data structure
type Heatmap struct {
	Version     string                `json:"version"`
	Metadata    Metadata              `json:"metadata"`
	Files       map[string]*FileHeat  `json:"files"`
	Incidents   []Incident            `json:"incidents"`
	Annotations map[string]Annotation `json:"annotations"`
}

// Metadata contains repository-level information
type Metadata struct {
	RepoPath    string         `json:"repo_path"`
	AnalyzedAt  time.Time      `json:"analyzed_at"`
	CommitSHA   string         `json:"commit_sha"`
	Branch      string         `json:"branch"`
	TotalFiles  int            `json:"total_files"`
	Languages   map[string]int `json:"languages"`
}

// FileHeat represents heat data for a single file
type FileHeat struct {
	Path               string             `json:"path"`
	HeatScore          int                `json:"heat_score"` // 0-100
	Tier               Tier               `json:"tier"`
	Language           string             `json:"language,omitempty"`
	Size               FileSize           `json:"size,omitempty"`
	Factors            Factors            `json:"factors"`
	Dependencies       Dependencies       `json:"dependencies,omitempty"`
	RecentChanges      []Change           `json:"recent_changes,omitempty"`
	ReviewRequirements ReviewRequirements `json:"review_requirements"`
}

// Tier represents risk tier
type Tier string

const (
	TierCritical Tier = "critical"
	TierHigh     Tier = "high"
	TierMedium   Tier = "medium"
	TierLow      Tier = "low"
)

// FileSize represents file size metrics
type FileSize struct {
	Lines int `json:"lines"`
	Bytes int `json:"bytes"`
}

// Factors contains all risk factor scores
type Factors struct {
	DependencyCentrality DependencyCentrality `json:"dependency_centrality"`
	IncidentHistory      IncidentHistory      `json:"incident_history"`
	ChangeFrequency      ChangeFrequency      `json:"change_frequency"`
	UserImpact           UserImpact           `json:"user_impact"`
	DataSensitivity      DataSensitivity      `json:"data_sensitivity"`
	TestCoverage         TestCoverage         `json:"test_coverage"`
	Complexity           Complexity           `json:"complexity"`
}

// DependencyCentrality measures how central a file is in the dependency graph
type DependencyCentrality struct {
	Score           float64 `json:"score"`             // 0-1
	ImportCount     int     `json:"import_count"`      // How many files import this
	ExportedSymbols int     `json:"exported_symbols"`  // How many symbols exported
}

// IncidentHistory tracks production incidents
type IncidentHistory struct {
	Score             int                `json:"score"` // 0-100
	IncidentCount     int                `json:"incident_count"`
	LastIncident      *time.Time         `json:"last_incident,omitempty"`
	SeverityBreakdown map[string]int     `json:"severity_breakdown"`
}

// ChangeFrequency measures code churn
type ChangeFrequency struct {
	Score           int `json:"score"` // 0-100
	CommitCount     int `json:"commit_count"`
	CommitsLast90d  int `json:"commits_last_90d"`
	UniqueAuthors   int `json:"unique_authors"`
}

// UserImpact measures user-facing impact
type UserImpact struct {
	Score           int  `json:"score"` // 0-100
	UserFacing      bool `json:"user_facing"`
	AffectsAuth     bool `json:"affects_auth"`
	AffectsData     bool `json:"affects_data"`
	AffectsPayments bool `json:"affects_payments"`
}

// DataSensitivity measures data handling risk
type DataSensitivity struct {
	Score            int  `json:"score"` // 0-100
	HandlesPII       bool `json:"handles_pii"`
	HandlesSecrets   bool `json:"handles_secrets"`
	HandlesFinancial bool `json:"handles_financial"`
}

// TestCoverage measures test protection
type TestCoverage struct {
	Score                 int     `json:"score"` // 0-100 (higher coverage = lower score)
	CoveragePercent       float64 `json:"coverage_percent"`
	TestCount             int     `json:"test_count"`
	HasIntegrationTests   bool    `json:"has_integration_tests"`
}

// Complexity measures code complexity
type Complexity struct {
	Score         int `json:"score"` // 0-100
	Cyclomatic    int `json:"cyclomatic"`
	Cognitive     int `json:"cognitive"`
	FunctionCount int `json:"function_count"`
}

// Dependencies tracks import relationships
type Dependencies struct {
	ImportedBy []Dependency `json:"imported_by,omitempty"`
	Imports    []Dependency `json:"imports,omitempty"`
}

// Dependency represents a single dependency relationship
type Dependency struct {
	Path      string `json:"path"`
	HeatScore int    `json:"heat_score"`
}

// Change represents a git commit
type Change struct {
	Date        time.Time `json:"date"`
	Message     string    `json:"message"`
	Author      string    `json:"author"`
	SHA         string    `json:"sha"`
	PRNumber    *int      `json:"pr_number,omitempty"`
	HadIncident bool      `json:"had_incident"`
}

// ReviewRequirements specifies required review depth
type ReviewRequirements struct {
	MinReviewers               int  `json:"min_reviewers"`
	RequiresSenior             bool `json:"requires_senior"`
	RequiresSecurityScan       bool `json:"requires_security_scan"`
	RequiresIntegrationTests   bool `json:"requires_integration_tests"`
	AutoMerge                  bool `json:"auto_merge"`
	EstimatedReviewTimeMinutes int  `json:"estimated_review_time_minutes"`
}

// Incident represents a production incident
type Incident struct {
	ID              string     `json:"id"`
	File            string     `json:"file"`
	Date            time.Time  `json:"date"`
	Severity        string     `json:"severity"`
	Description     string     `json:"description"`
	CausedByCommit  string     `json:"caused_by_commit,omitempty"`
	FixedByCommit   string     `json:"fixed_by_commit,omitempty"`
	DowntimeMinutes int        `json:"downtime_minutes,omitempty"`
	UsersAffected   int        `json:"users_affected,omitempty"`
}

// Annotation represents manual metadata
type Annotation struct {
	Path             string   `json:"path"`
	Tags             []string `json:"tags,omitempty"`
	Notes            string   `json:"notes,omitempty"`
	OverrideTier     *Tier    `json:"override_tier,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	DocumentationURL string   `json:"documentation_url,omitempty"`
}

// PRRisk represents risk assessment for a pull request
type PRRisk struct {
	PRNumber              int                `json:"pr_number"`
	HeatScore             int                `json:"heat_score"` // Max score of changed files
	Tier                  Tier               `json:"tier"`
	FilesChanged          []FileChange       `json:"files_changed"`
	ReviewRequirements    ReviewRequirements `json:"review_requirements"`
	CircuitBreakerSignals []string           `json:"circuit_breaker_signals,omitempty"`
}

// FileChange represents a changed file in a PR
type FileChange struct {
	Path      string `json:"path"`
	HeatScore int    `json:"heat_score"`
	Tier      Tier   `json:"tier"`
	LinesAdded int   `json:"lines_added"`
	LinesDeleted int `json:"lines_deleted"`
	IsNew     bool   `json:"is_new"`
}

// Config represents heatmap configuration
type Config struct {
	Version     string                        `yaml:"version"`
	Tiers       TierThresholds                `yaml:"tiers"`
	Requirements map[Tier]ReviewRequirements  `yaml:"review_requirements"`
	CircuitBreakers CircuitBreakerConfig      `yaml:"circuit_breakers"`
	Notifications NotificationConfig           `yaml:"notifications"`
	GitHub      GitHubConfig                  `yaml:"github"`
}

// TierThresholds defines heat score thresholds for each tier
type TierThresholds struct {
	Critical int `yaml:"critical"` // >= 86
	High     int `yaml:"high"`     // >= 61
	Medium   int `yaml:"medium"`   // >= 31
	Low      int `yaml:"low"`      // >= 0
}

// CircuitBreakerConfig defines PR rejection rules
type CircuitBreakerConfig struct {
	MaxDiffLines        int     `yaml:"max_diff_lines"`
	MaxLanguages        int     `yaml:"max_languages"`
	MinTestRatio        float64 `yaml:"min_test_ratio"`
	RequirePRDescription bool   `yaml:"require_pr_description"`
}

// NotificationConfig defines notification settings
type NotificationConfig struct {
	Slack SlackConfig `yaml:"slack"`
	Email EmailConfig `yaml:"email"`
}

// SlackConfig defines Slack notification settings
type SlackConfig struct {
	Enabled      bool   `yaml:"enabled"`
	WebhookURL   string `yaml:"webhook_url"`
	NotifyOnTier []Tier `yaml:"notify_on_tier"`
}

// EmailConfig defines email notification settings
type EmailConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Recipients   []string `yaml:"recipients"`
	DailyDigest  bool     `yaml:"daily_digest"`
}

// GitHubConfig defines GitHub integration settings
type GitHubConfig struct {
	PostPRComments       bool   `yaml:"post_pr_comments"`
	UpdateOnPush         bool   `yaml:"update_on_push"`
	BlockAutoMergeOnTier []Tier `yaml:"block_auto_merge_on_tier"`
}
