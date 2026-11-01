package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/dashboard"
	"github.com/zanetworker/highstakes/internal/incidents"
	"github.com/zanetworker/highstakes/internal/policy"
	"github.com/zanetworker/highstakes/internal/prcomment"
	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/prscore"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/tui"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

var version = "1.0.0"

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(exitCode(err))
	}
}

var rootCmd = &cobra.Command{
	Use:           "highstakes",
	Short:         "Find high-stakes code that needs human review",
	SilenceErrors: true,
	SilenceUsage:  true,
	Version:       version,
	RunE: func(cmd *cobra.Command, args []string) error {
		return launchTUI()
	},
}

// --- TUI ---

func launchTUI() error {
	hm, err := loadHeatmap()
	if err != nil {
		return err
	}
	m := tui.NewModel(hm)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// --- init ---

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize heatmap in current repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")

		if err := os.MkdirAll(".heatmap", 0755); err != nil {
			return cliError(1, "create .heatmap directory: %v", err)
		}

		config := heatmap.DefaultConfig()
		if err := heatmap.SaveConfig(config, filepath.Join(".heatmap", "config.yaml")); err != nil {
			return cliError(1, "save config: %v", err)
		}

		if jsonOut {
			return printJSON(map[string]interface{}{
				"initialized": true,
				"config_path": ".heatmap/config.yaml",
			})
		}

		fmt.Fprintln(os.Stderr, "Initialized .heatmap/ directory")
		fmt.Fprintln(os.Stderr, "Next: run 'highstakes analyze'")
		return nil
	},
}

// --- analyze ---

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze repository and generate heatmap",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		noLLM, _ := cmd.Flags().GetBool("no-llm")
		force, _ := cmd.Flags().GetBool("force")
		model, _ := cmd.Flags().GetString("model")
		concurrency, _ := cmd.Flags().GetInt("concurrency")
		jsonOut, _ := cmd.Flags().GetBool("json")
		if _, err := loadConfigOrDefault(); err != nil {
			return err // invalid config exits 2, as in every other command
		}

		gen, err := heatmap.New(cwd, heatmap.GenerateOptions{
			NoLLM:         noLLM,
			ForceReassess: force,
			Model:         model,
			Concurrency:   concurrency,
		})
		if err != nil {
			return err
		}

		hm, err := gen.Generate()
		if err != nil {
			return err
		}

		heatmapPath := filepath.Join(".heatmap", "heatmap.json")
		if err := heatmap.Save(hm, heatmapPath); err != nil {
			return err
		}

		if jsonOut {
			return printJSON(map[string]interface{}{
				"path":        heatmapPath,
				"total_files": hm.Metadata.TotalFiles,
				"commit":      hm.Metadata.CommitSHA,
				"tiers":       tierCounts(hm),
			})
		}

		fmt.Fprintf(os.Stderr, "\nHeatmap generated: %s (%d files)\n", heatmapPath, hm.Metadata.TotalFiles)
		tc := tierCounts(hm)
		fmt.Fprintf(os.Stderr, "\nHeat Distribution:\n")
		fmt.Fprintf(os.Stderr, "  🔥🔥🔥 CRITICAL: %d files\n", tc["critical"])
		fmt.Fprintf(os.Stderr, "  🔥🔥  HIGH:     %d files\n", tc["high"])
		fmt.Fprintf(os.Stderr, "  🔥   MEDIUM:   %d files\n", tc["medium"])
		fmt.Fprintf(os.Stderr, "  🟢   LOW:      %d files\n", tc["low"])
		return nil
	},
}

// --- score ---

var scoreCmd = &cobra.Command{
	Use:   "get <file>",
	Short: "Get a file's verdict, both questions with repo cutoffs, and what to do",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		file, ok := hm.Files[args[0]]
		if !ok {
			available := make([]string, 0, 5)
			for p := range hm.Files {
				if strings.Contains(p, args[0]) {
					available = append(available, p)
					if len(available) >= 5 {
						break
					}
				}
			}
			hint := ""
			if len(available) > 0 {
				hint = fmt.Sprintf(". Did you mean: %s", strings.Join(available, ", "))
			}
			return cliError(4, "file not found in heatmap: %s%s", args[0], hint)
		}

		if jsonOut {
			// The file's JSON with the repo cutoffs its scores read against.
			return printJSON(struct {
				*types.FileHeat
				LikelyCutoff    int `json:"likely_cutoff"`
				ImpactThreshold int `json:"impact_threshold"`
			}{file, hm.Metadata.LikelyCutoff, hm.Metadata.ImpactThreshold})
		}

		printFileScore(file, hm.Metadata)
		return nil
	},
}

func printFileScore(file *types.FileHeat, md types.Metadata) {
	if block := explanationBlock(file, md.LikelyCutoff, md.ImpactThreshold); block != "" {
		fmt.Print(block)
		return
	}

	// Heatmaps written before explanations existed.
	fmt.Printf("%s  %s %s  (score: %d)\n\n",
		file.Path, tierEmoji(file.Tier), strings.ToUpper(string(file.Tier)), file.HeatScore)

	fmt.Println(axesLines(file))

	// Blast radius reasoning (the key insight)
	br := file.Factors.BlastRadius
	if br.Assessed {
		fmt.Println("Blast Radius (LLM-assessed):")
		if br.Summary != "" {
			fmt.Printf("  %s\n", br.Summary)
		}
		if br.CriticalReason != "" {
			fmt.Printf("  Reason: %s\n", br.CriticalReason)
		}
		fmt.Printf("  Security: %d  Data: %d  Availability: %d  User: %d\n\n",
			br.SecurityImpact, br.DataImpact, br.AvailabilityImpact, br.UserImpact)
	}

	// Breakage impact
	importedBy := len(file.Dependencies.ImportedBy)
	if importedBy > 0 {
		fmt.Printf("Breakage Impact: %d files break if this changes\n\n", importedBy)
	}

	fmt.Println("Risk Factors:")
	fmt.Printf("  Dependency Centrality:  %.0f (%d imports)\n",
		file.Factors.DependencyCentrality.Score*100,
		file.Factors.DependencyCentrality.ImportCount)
	fmt.Printf("  Change Frequency:       %d (%d commits/90d)\n",
		file.Factors.ChangeFrequency.Score,
		file.Factors.ChangeFrequency.CommitsLast90d)
	fmt.Printf("  Complexity:             %d (cyclomatic: %d)\n",
		file.Factors.Complexity.Score,
		file.Factors.Complexity.Cyclomatic)
	fmt.Println()
	printReview(file.ReviewRequirements)
}

// printReview prints the tier's review requirements on one line.
func printReview(req types.ReviewRequirements) {
	fmt.Printf("Review: %d reviewers", req.MinReviewers)
	if req.RequiresSenior {
		fmt.Print(" (senior)")
	}
	if req.RequiresSecurityScan {
		fmt.Print(", security scan")
	}
	fmt.Printf(", ~%d min", req.EstimatedReviewTimeMinutes)
	if req.AutoMerge {
		fmt.Print(", auto-merge OK")
	} else {
		fmt.Print(", auto-merge blocked")
	}
	fmt.Println()
}

// axesLines states a file's two axes as questions, one line each.
func axesLines(f *types.FileHeat) string {
	impact := fmt.Sprintf("%-22s %d  (LLM blast radius)", types.AxisImpact, f.Impact)
	if !f.ImpactAssessed {
		impact = fmt.Sprintf("%-22s ~%d (estimate from centrality and path keywords; analyze with an LLM to assess)", types.AxisImpact, f.Impact)
	}
	lines := fmt.Sprintf("%-22s %d  (churn, complexity, centrality, incidents)", types.AxisLikelihood, f.Likelihood) + "\n" + impact
	if l := f.Quadrant.Label(); l != "" {
		lines += "\n" + l
	}
	return lines + "\n"
}

// --- list ---

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List files by heat, likelihood or impact",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		tier, _ := cmd.Flags().GetString("tier")
		limit, _ := cmd.Flags().GetInt("limit")
		sortBy, _ := cmd.Flags().GetString("sort")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		// Validate tier flag
		validTiers := map[string]bool{"critical": true, "high": true, "medium": true, "low": true, "": true}
		if !validTiers[tier] {
			return cliError(2, "--tier must be one of: critical, high, medium, low (got: %q)", tier)
		}

		var files []listEntry
		for path, f := range hm.Files {
			if tier != "" && string(f.Tier) != tier {
				continue
			}
			reason := ""
			if f.Factors.BlastRadius.Assessed && f.Factors.BlastRadius.Summary != "" {
				reason = f.Factors.BlastRadius.Summary
			}
			files = append(files, listEntry{path, f.HeatScore, string(f.Tier), reason,
				f.Likelihood, f.Impact, f.ImpactAssessed, f.Quadrant})
		}

		if err := sortListEntries(files, sortBy); err != nil {
			return cliError(2, "--sort: %v", err)
		}

		if limit > 0 && len(files) > limit {
			files = files[:limit]
		}

		if jsonOut {
			return printJSON(map[string]interface{}{
				"files":     files,
				"count":     len(files),
				"truncated": limit > 0 && len(hm.Files) > limit,
			})
		}

		for _, f := range files {
			emoji := tierEmoji(types.Tier(f.Tier))
			estimate := " "
			if !f.ImpactAssessed {
				estimate = "~"
			}
			line := fmt.Sprintf("%s %3d  L %3d  I%s%3d  %-50s", emoji, f.Score, f.Likelihood, estimate, f.Impact, f.Path)
			if f.Reason != "" {
				line += "  " + f.Reason
			}
			fmt.Println(line)
		}

		return nil
	},
}

// listEntry is one row of `list`. Fields after Reason were added with the
// two axes; the earlier ones keep their JSON names.
type listEntry struct {
	Path           string         `json:"path"`
	Score          int            `json:"heat_score"`
	Tier           string         `json:"tier"`
	Reason         string         `json:"reason,omitempty"`
	Likelihood     int            `json:"likelihood"`
	Impact         int            `json:"impact"`
	ImpactAssessed bool           `json:"impact_assessed"`
	Quadrant       types.Quadrant `json:"quadrant,omitempty"`
}

// sortListEntries orders entries by heat, likelihood or impact (descending,
// ties by path).
func sortListEntries(files []listEntry, by string) error {
	key := map[string]func(listEntry) int{
		"heat":       func(e listEntry) int { return e.Score },
		"likelihood": func(e listEntry) int { return e.Likelihood },
		"impact":     func(e listEntry) int { return e.Impact },
	}[by]
	if key == nil {
		return fmt.Errorf("invalid value %q. Valid values: heat, likelihood, impact", by)
	}
	sort.Slice(files, func(i, j int) bool {
		if a, b := key(files[i]), key(files[j]); a != b {
			return a > b
		}
		return files[i].Path < files[j].Path
	})
	return nil
}

// configuredTiers returns the tier thresholds from .heatmap/config.yaml, or the
// defaults when the file is missing, unreadable or sets no tiers.
func configuredTiers() types.TierThresholds {
	if cfg, err := heatmap.LoadConfig(filepath.Join(".heatmap", "config.yaml")); err == nil && cfg.Tiers.Critical > 0 {
		return cfg.Tiers
	}
	return scorer.DefaultTierThresholds()
}

// --- pr ---

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Assess pull request risk",
}

var prCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check risk of current diff against base branch",
	RunE: func(cmd *cobra.Command, args []string) error {
		base, _ := cmd.Flags().GetString("base")
		jsonOut, _ := cmd.Flags().GetBool("json")
		format, _ := cmd.Flags().GetString("format")
		shadow, _ := cmd.Flags().GetBool("shadow")
		failOn, _ := cmd.Flags().GetString("fail-on")
		authorFlag, _ := cmd.Flags().GetString("author")
		maxReviewers, _ := cmd.Flags().GetInt("reviewers")
		approvals, _ := cmd.Flags().GetStringSlice("approvals")
		repoFlag, _ := cmd.Flags().GetString("repo")
		prFlag, _ := cmd.Flags().GetInt("pr")

		if jsonOut {
			format = "json"
		}
		if format != "text" && format != "markdown" && format != "json" {
			return cliError(2, "--format must be one of: text, markdown, json (got: %q)", format)
		}

		repo, err := commentRepo(repoFlag, os.Getenv("GITHUB_REPOSITORY"), func() (prdata.Repo, error) { return prdata.OriginRepo(".") })
		if err != nil {
			return cliError(2, "%v", err)
		}
		pr, err := prNumber(prFlag, os.Getenv("GITHUB_REF"))
		if err != nil {
			return cliError(2, "%v", err)
		}

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		// --no-renames: a rename is its old path deleted plus its new path added,
		// so every path is a real one (no "a => b" forms).
		out, err := exec.Command("git", "diff", "--no-renames", "--numstat", base+"...HEAD").Output()
		if err != nil {
			return cliError(3, "git diff failed against base %q: %v. Valid bases: main, origin/main, HEAD~N", base, err)
		}

		added, removed := diffPaths(base, "A"), diffPaths(base, "D")
		var changes []prscore.FileChange
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 3 {
				continue
			}
			linesAdded, _ := strconv.Atoi(parts[0])
			deleted, _ := strconv.Atoi(parts[1])
			changes = append(changes, prscore.FileChange{
				Path: parts[2], LinesAdded: linesAdded, LinesDeleted: deleted, IsNew: added[parts[2]], Deleted: removed[parts[2]],
			})
		}

		// An empty diff scores LOW and still goes through the gate and shadow log.
		risk := prscore.CalculatePRRisk(pr, changes, hm, configuredTiers())
		applyAutonomy(&risk, hm)
		addReviewers(&risk, prAuthor(authorFlag), maxReviewers)
		codeowners, _ := policy.LoadCodeowners(".") // CODEOWNERS at HEAD; missing means nothing is covered
		attachPRActions(&risk, hm, buildChange(changes, hm, approvals, codeowners.Covers))
		gateErr := applyGate(&risk, failOn, shadow)
		if ee, ok := gateErr.(*exitError); ok && ee.code == 2 {
			return gateErr
		}
		if shadow {
			if err := appendShadowLog(shadowLogPath, base, &risk); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			fmt.Fprintf(os.Stderr, "shadow: tier=%s change_risk=%s decision=%s (not enforced, logged to %s)\n",
				risk.Tier, risk.ChangeRiskLevel, risk.Decision, shadowLogPath)
		}

		switch {
		case format == "markdown":
			fmt.Print(prcomment.Render(&risk, hm, prcomment.Links{Repo: repo, Base: linkBase(base), Codeowners: codeownersPath(), SHA: headSHA()}))
			return gateErr
		case format == "json" && len(changes) == 0:
			empty := map[string]interface{}{"changes": 0, "tier": risk.Tier, "verdict": risk.Verdict}
			if risk.Decision != "" {
				empty["decision"], empty["fail_on"], empty["shadow"] = risk.Decision, risk.FailOn, risk.Shadow
			}
			if err := printJSON(empty); err != nil {
				return err
			}
			return gateErr
		case format == "json":
			if err := printJSON(risk); err != nil {
				return err
			}
			return gateErr
		case len(changes) == 0:
			fmt.Println("No changes detected against", base)
			printDecision(&risk)
			return gateErr
		}

		printPRText(&risk)
		return gateErr
	},
}

// --- incident ---

var incidentCmd = &cobra.Command{
	Use:   "incident",
	Short: "Manage incident records",
}

var incidentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Record a new incident",
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		severity, _ := cmd.Flags().GetString("severity")
		desc, _ := cmd.Flags().GetString("description")
		dateStr, _ := cmd.Flags().GetString("date")
		jsonOut, _ := cmd.Flags().GetBool("json")

		if file == "" || severity == "" || desc == "" {
			return cliError(2, "--file, --severity, and --description are required")
		}

		date := time.Now()
		if dateStr != "" {
			parsed, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return cliError(2, "--date must be YYYY-MM-DD (got: %q)", dateStr)
			}
			date = parsed
		}

		store, err := incidents.NewStore(filepath.Join(".heatmap", "incidents.json"))
		if err != nil {
			return err
		}

		inc, err := store.Add(file, severity, desc, date)
		if err != nil {
			return err
		}

		if jsonOut {
			return printJSON(inc)
		}

		fmt.Fprintf(os.Stderr, "Recorded %s (%s, %s, %s)\n", inc.ID, inc.File, inc.Severity, inc.Date.Format("2006-01-02"))
		fmt.Fprintln(os.Stderr, "Run 'highstakes analyze' to update scores.")
		return nil
	},
}

var incidentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recorded incidents",
	RunE: func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		jsonOut, _ := cmd.Flags().GetBool("json")

		store, err := incidents.NewStore(filepath.Join(".heatmap", "incidents.json"))
		if err != nil {
			return err
		}

		var result []types.Incident
		if file != "" {
			result = store.ForFile(file)
		} else {
			result = store.All()
		}

		if jsonOut {
			return printJSON(result)
		}

		if len(result) == 0 {
			fmt.Println("No incidents recorded.")
			return nil
		}

		for _, inc := range result {
			fmt.Printf("  %s  %s  %-8s  %s\n", inc.Date.Format("2006-01-02"), inc.ID, inc.Severity, inc.File)
			if inc.Description != "" {
				fmt.Printf("    %s\n", inc.Description)
			}
		}
		return nil
	},
}

// --- report ---

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate heat distribution report",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		limit, _ := cmd.Flags().GetInt("limit")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		if jsonOut {
			return printJSON(hm)
		}

		type scored struct {
			path   string
			score  int
			tier   string
			reason string
		}
		var files []scored
		for path, f := range hm.Files {
			reason := ""
			if f.Factors.BlastRadius.Summary != "" {
				reason = f.Factors.BlastRadius.Summary
			}
			files = append(files, scored{path, f.HeatScore, string(f.Tier), reason})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].score > files[j].score })

		fmt.Printf("Code Heatmap Report  (%d files, %s)\n\n",
			hm.Metadata.TotalFiles, hm.Metadata.AnalyzedAt.Format("2006-01-02"))

		for _, tier := range []string{"critical", "high", "medium", "low"} {
			var group []scored
			for _, f := range files {
				if f.tier == tier {
					group = append(group, f)
				}
			}
			if len(group) == 0 {
				continue
			}

			fmt.Printf("%s %s (%d files)\n", tierEmoji(types.Tier(tier)), strings.ToUpper(tier), len(group))

			show := limit
			if show <= 0 || show > len(group) {
				show = len(group)
			}
			for _, f := range group[:show] {
				line := fmt.Sprintf("  %3d  %-50s", f.score, f.path)
				if f.reason != "" {
					line += "  " + f.reason
				}
				fmt.Println(line)
			}
			if len(group) > show {
				fmt.Printf("  ... and %d more\n", len(group)-show)
			}
			fmt.Println()
		}

		totalMinutes := 0
		for _, f := range hm.Files {
			totalMinutes += f.ReviewRequirements.EstimatedReviewTimeMinutes
		}
		fmt.Printf("Total estimated review time: %.1f hours\n", float64(totalMinutes)/60)
		return nil
	},
}

// --- dashboard ---

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Generate interactive HTML dashboard and open in browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		noOpen, _ := cmd.Flags().GetBool("no-open")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		if output == "" {
			output = filepath.Join(".heatmap", "dashboard.html")
		}
		decl, err := loadDeclared(hm)
		if err != nil {
			return err
		}

		if err := dashboard.Generate(hm, decl, output); err != nil {
			return err
		}

		absPath, _ := filepath.Abs(output)
		fmt.Fprintf(os.Stderr, "Dashboard generated: %s\n", absPath)

		if !noOpen {
			_ = exec.Command("open", absPath).Start()
		}

		return nil
	},
}

// --- agent-context ---

var agentContextCmd = &cobra.Command{
	Use:   "agent-context",
	Short: "Machine-readable description of all commands and flags",
	RunE: func(cmd *cobra.Command, args []string) error {
		return printJSON(agentContext())
	},
}

// agentContext describes every command and flag for agent callers.
// TestAgentContext_CoversEveryCommandAndFlag keeps it in step with rootCmd.
func agentContext() map[string]interface{} {
	return map[string]interface{}{
		"schema_version": "1",
		"cli_version":    version,
		"commands": map[string]interface{}{
			"init": map[string]interface{}{
				"description": "Initialize heatmap in current repository",
				"flags":       map[string]interface{}{"--json": flagDef("bool", false, "Output as JSON")},
			},
			"analyze": map[string]interface{}{
				"description": "Analyze repository and generate heatmap with LLM blast radius assessment",
				"flags": map[string]interface{}{
					"--json":        flagDef("bool", false, "Output as JSON"),
					"--no-llm":      flagDef("bool", false, "Skip LLM, use static heuristics only"),
					"--force":       flagDef("bool", false, "Re-assess all files (ignore cache)"),
					"--model":       flagDef("string", "openai/gpt-6-luna", "OpenRouter model ID"),
					"--concurrency": flagDef("int", 10, "Parallel LLM requests"),
				},
			},
			"get": map[string]interface{}{
				"description": "Get a file's verdict (Harden now, Protect, Stabilize, Routine), Will it break? and How bad if it breaks? with repo cutoffs, before-merge and backlog actions (JSON: explanation.actions with scope, signal, command; likely_cutoff, impact_threshold)",
				"args":        "file_path (required)",
				"flags":       map[string]interface{}{"--json": flagDef("bool", false, "Output as JSON")},
				"subcommands": map[string]interface{}{
					"tour": map[string]interface{}{
						"description": "Reading path with file:line anchors: product sections ranked by importance (max impact, fragile-critical stops, max heat), each section's dependencies emitted before it; supporting sections last; files by impact then centrality",
						"flags": map[string]interface{}{
							"--json":               flagDef("bool", false, "Output as JSON (schema version 1); same as --format json"),
							"--format":             flagDef("enum", "text", "Values: text, json, walkthrough (show-me step-through data)"),
							"--out":                flagDef("string", "", "Write to this file instead of stdout"),
							"--limit":              flagDef("int", 0, "Keep the N most impactful files (0 = all); ignored with --per-component"),
							"--per-component":      flagDef("int", 0, "Keep the N most important files of each component, one section per component (0 = off)"),
							"--min-impact":         flagDef("int", 0, "Skip files whose impact is below this, and components left without files (0-100)"),
							"--components":         flagDef("string", "", "Only these components, comma-separated (e.g. crates/core,internal/api)"),
							"--include-supporting": flagDef("bool", false, "Add supporting components (examples, scripts, .github, docs, deploy, tests, vendor; tour.supporting in config) in a final part"),
							"--focus":              flagDef("string", "", "Only files under this repo-relative directory or file"),
						},
					},
					"architecture": map[string]interface{}{
						"description": "Generated architecture diagram of runtime units: SeeCode spec or plain JSON, with declared links from .heatmap/architecture.yaml and top files per box as evidence",
						"flags": map[string]interface{}{
							"--format":             flagDef("enum", "seecode", "Values: seecode, json"),
							"--json":               flagDef("bool", false, "Same as --format json"),
							"--max-nodes":          flagDef("int", 9, "Most boxes (2-24); the rest fold into one other-units box"),
							"--out":                flagDef("string", "", "Write to this file instead of stdout"),
							"--include-supporting": flagDef("bool", false, "Include examples, scripts, tests and helper bins"),
						},
					},
				},
			},
			"list": map[string]interface{}{
				"description": "List files sorted by heat score",
				"flags": map[string]interface{}{
					"--json":  flagDef("bool", false, "Output as JSON"),
					"--tier":  flagDef("enum", "", "Filter by tier. Values: critical, high, medium, low"),
					"--limit": flagDef("int", 0, "Max files to return (0 = all)"),
					"--sort":  flagDef("enum", "heat", "Values: heat, likelihood, impact"),
				},
				"subcommands": map[string]interface{}{
					"tasks": map[string]interface{}{
						"description": "Backlog items on files at or above an agent autonomy level, as self-contained tasks (JSON: tasks[] with title, file, action_id, signal, command, acceptance, level, level_label; count, min_level, max_level). Levels: 3 auto-merge, 2 auto-fix with a human merging, 1 propose only, 0 assist only; files without an LLM-assessed impact are capped at 1, and autonomy.max_level caps all",
						"flags": map[string]interface{}{
							"--json":  flagDef("bool", false, "Output as JSON"),
							"--level": flagDef("int", 2, "Lowest autonomy level to include. Values: 0, 1, 2, 3"),
						},
					},
					"units": map[string]interface{}{
						"description": "Runtime units (binaries, scripts) with linked components, max impact, fragile-critical count and inferred placement (file:line evidence)",
						"flags": map[string]interface{}{
							"--json":               flagDef("bool", false, "Output as JSON"),
							"--include-supporting": flagDef("bool", false, "Include examples, scripts, tests and helper bins"),
						},
					},
				},
			},
			"pr": map[string]interface{}{
				"description": "Assess pull request risk",
				"subcommands": map[string]interface{}{
					"check": map[string]interface{}{
						"description": "Score current diff against a base branch: verdict (Harden now, Protect, Stabilize, Routine), before-merge checklist of Harden now and Protect files with done items, worth-doing items that do not block (JSON worth_doing), backlog, and agent autonomy (JSON autonomy_level: lowest level over touched files; auto_merge_allowed: true only at level 3; autonomy_reason; files_changed[].autonomy)",
						"flags": map[string]interface{}{
							"--json":      flagDef("bool", false, "Output as JSON (same as --format json)"),
							"--format":    flagDef("enum", "text", "Values: text, markdown (the PR comment the triage workflow posts), json"),
							"--approvals": flagDef("string", "", "Comma-separated GitHub logins that approved the PR; a suggested owner's approval ticks owner-review items"),
							"--repo":      flagDef("string", "", "GitHub owner/name the markdown comment links to (one-click issues, CODEOWNERS edit); default $GITHUB_REPOSITORY, then the origin remote; invalid exits 2"),
							"--pr":        flagDef("int", 0, "PR number the comment's issues link back to; default from $GITHUB_REF (refs/pull/N/merge), else no link; negative exits 2"),
							"--base":      flagDef("string", "main", "Base branch"),
							"--fail-on":   flagDef("enum", "", "Exit 5 when tier >= this. Values: critical, high, medium, low"),
							"--shadow":    flagDef("bool", false, "Log decision to .heatmap/shadow.jsonl, always exit 0"),
							"--author":    flagDef("string", "", "Author to exclude from suggested reviewers (default: HEAD author)"),
							"--reviewers": flagDef("int", 3, "Max suggested reviewers (0 = off)"),
						},
					},
				},
			},
			"create": map[string]interface{}{
				"description": "Create review artifacts from the heatmap",
				"subcommands": map[string]interface{}{
					"codeowners": map[string]interface{}{
						"description": "CODEOWNERS block for high-stakes paths with owners from git history",
						"flags": map[string]interface{}{
							"--json":       flagDef("bool", false, "Output as JSON"),
							"--write":      flagDef("bool", false, "Update the marked block in --file"),
							"--file":       flagDef("string", ".github/CODEOWNERS", "CODEOWNERS path for --write"),
							"--min-tier":   flagDef("enum", "high", "Values: critical, high, medium, low"),
							"--max-owners": flagDef("int", 2, "Owners per path"),
						},
					},
					"labels": map[string]interface{}{
						"description": "Blind CSV sheet (path, maintainer_rating, notes) for maintainers to rate 1-5 how bad a bug in each file would be; feed it to check calibration --labels",
						"flags": map[string]interface{}{
							"--json":   flagDef("bool", false, "Output the sampled paths and settings as JSON"),
							"--top":    flagDef("int", 20, "Files HighStakes rates highest on blast radius"),
							"--random": flagDef("int", 20, "Randomly sampled other non-test files"),
							"--seed":   flagDef("int", 1, "Random seed; the same seed gives the same sheet"),
							"--out":    flagDef("string", "", "Write the CSV to this file (default stdout)"),
						},
					},
					"policy": map[string]interface{}{
						"description": "No-auto-approve path policy for AI reviewers (YAML; JSON with --json). With --agents: the agent autonomy policy, path globs per level (3 auto-merge, 2 auto-fix with a human merging, 1 propose only, 0 assist only) with how_to_read, cap_rule, max_level and default_level; a change may go as far as the lowest level among its files",
						"flags": map[string]interface{}{
							"--json":     flagDef("bool", false, "Output as JSON (same as --format json)"),
							"--min-tier": flagDef("enum", "high", "Values: critical, high, medium, low (ignored with --agents)"),
							"--agents":   flagDef("bool", false, "Emit the agent autonomy policy instead of the review policy"),
							"--format":   flagDef("enum", "yaml", "Values: yaml, json"),
							"--out":      flagDef("string", "", "Write to this file instead of stdout (e.g. .highstakes/autonomy.yaml)"),
						},
					},
				},
			},
			"incident": map[string]interface{}{
				"description": "Manage production incident records",
				"subcommands": map[string]interface{}{
					"create": map[string]interface{}{
						"description": "Record a new incident",
						"flags": map[string]interface{}{
							"--json":        flagDef("bool", false, "Output as JSON"),
							"--file":        flagDef("string", "", "File path (required)"),
							"--severity":    flagDef("enum", "", "Values: critical, high, medium, low (required)"),
							"--description": flagDef("string", "", "What happened (required)"),
							"--date":        flagDef("string", "", "YYYY-MM-DD (default: today)"),
						},
					},
					"list": map[string]interface{}{
						"description": "List recorded incidents",
						"flags": map[string]interface{}{
							"--json": flagDef("bool", false, "Output as JSON"),
							"--file": flagDef("string", "", "Filter by file path"),
						},
					},
				},
			},
			"check": map[string]interface{}{
				"description": "Check how well the heatmap matches reality",
				"subcommands": map[string]interface{}{
					"calibration": map[string]interface{}{
						"description": "Compare heat scores with reverts, bug fixes, security fixes and incidents in git history, or with merged GitHub pull requests (--source prs)",
						"flags": map[string]interface{}{
							"--json":    flagDef("bool", false, "Output as JSON"),
							"--top":     flagDef("int", 10, "K for precision@K and recall@K"),
							"--since":   flagDef("string", "365d", "Event window in days (Nd)"),
							"--holdout": flagDef("string", "", "Cutoff YYYY-MM-DD or Nd: score with history before it, count events after it"),
							"--limit":   flagDef("int", 10, "Max files per surprise list"),
							"--events":  flagDef("enum", "all", "Event kinds to count. Values: all, fixes, reverts, security"),
							"--labels":  flagDef("string", "", "Compare with maintainer ratings in this CSV (from create labels) instead of git history"),
							"--source":  flagDef("enum", "commits", "Where events come from. Values: commits (commit-message keywords), prs (merged GitHub PRs via gh: bug-fix/revert PRs vs likelihood, backports, fast reverts, severity labels, review depth and CODEOWNERS vs blast radius; cached in .heatmap/cache/github/)"),
							"--repo":    flagDef("string", "", "GitHub owner/name for --source prs (default: from the origin remote)"),
						},
					},
				},
			},
			"dashboard": map[string]interface{}{
				"description": "Generate the static HTML dashboard (action inbox by default; treemap, explorer, architecture, quadrant tabs) and open it in the browser",
				"flags": map[string]interface{}{
					"--output":  flagDef("string", "", "Output path (default: .heatmap/dashboard.html)"),
					"--no-open": flagDef("bool", false, "Don't open in browser"),
				},
			},
			"agent-context": map[string]interface{}{
				"description": "Machine-readable description of all commands and flags (this document)",
			},
			"report": map[string]interface{}{
				"description": "Generate heat distribution report with reasoning",
				"flags": map[string]interface{}{
					"--json":  flagDef("bool", false, "Output full heatmap as JSON"),
					"--limit": flagDef("int", 10, "Max files per tier"),
				},
			},
			"github": map[string]interface{}{
				"description": "GitHub integration",
				"subcommands": map[string]interface{}{
					"install": map[string]interface{}{
						"description": "Write .github/workflows/heatmap-triage.yml: posts pr check --format markdown as a PR comment, fills --approvals from reviews, fails on Harden now files (--fail-on critical)",
					},
				},
			},
		},
		"environment": map[string]interface{}{
			"OPENROUTER_API_KEY": "Required for LLM blast radius analysis. Get from openrouter.ai",
		},
		"models": []string{
			"openai/gpt-6-luna (default, best impact ranking on the benchmark)",
			"z-ai/glm-5.3-flash (cheapest, slower)",
			"deepseek/deepseek-v4.1-flash (open weights, self-hostable)",
			"anthropic/claude-haiku-5.5 (fewest failed calls)",
		},
		"exit_codes": map[string]interface{}{
			"0": "Success",
			"1": "Internal error",
			"2": "Invalid input (bad flags, missing required params)",
			"3": "External dependency failure (git, API)",
			"4": "Not found (file not in heatmap)",
			"5": "PR blocked by pr check --fail-on",
		},
	}
}

// --- github ---

// triageWorkflow is the workflow `github install` writes: the repo's own
// heatmap-triage.yml, installing highstakes from the module instead of ./.
//
//go:embed triage-workflow.yml
var triageWorkflow string

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "GitHub integration commands",
}

var githubInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install GitHub Action for automated PR triage",
	RunE: func(cmd *cobra.Command, args []string) error {
		workflowDir := filepath.Join(".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0755); err != nil {
			return err
		}

		workflowPath := filepath.Join(workflowDir, "heatmap-triage.yml")
		if err := os.WriteFile(workflowPath, []byte(triageWorkflow), 0644); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Created %s\nCommit and push to activate.\n", workflowPath)
		return nil
	},
}

// --- helpers ---

func loadHeatmap() (*types.Heatmap, error) {
	hm, err := heatmap.Load(".heatmap/heatmap.json")
	if err != nil {
		return nil, cliError(4, "no heatmap found. Run: highstakes init && highstakes analyze")
	}
	return hm, nil
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func tierEmoji(tier types.Tier) string {
	switch tier {
	case types.TierCritical:
		return "🔥🔥🔥"
	case types.TierHigh:
		return "🔥🔥"
	case types.TierMedium:
		return "🔥"
	default:
		return "🟢"
	}
}

func tierCounts(hm *types.Heatmap) map[string]int {
	counts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, f := range hm.Files {
		counts[string(f.Tier)]++
	}
	return counts
}

func flagDef(typ string, def interface{}, desc string) map[string]interface{} {
	return map[string]interface{}{"type": typ, "default": def, "description": desc}
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func cliError(code int, format string, args ...interface{}) error {
	return &exitError{code: code, msg: fmt.Sprintf(format, args...)}
}

// exitCode maps an error to the documented exit code (1 when unclassified).
func exitCode(err error) int {
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	return 1
}

// --- command registration ---

func init() {
	// Structured exit codes
	cobra.AddTemplateFunc("exit", func() {})
	rootCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) {}

	initCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(initCmd)

	analyzeCmd.Flags().Bool("no-llm", false, "Skip LLM analysis, use static heuristics only")
	analyzeCmd.Flags().Bool("force", false, "Re-assess all files (ignore cache)")
	analyzeCmd.Flags().String("model", "", "OpenRouter model (default: openai/gpt-6-luna)")
	analyzeCmd.Flags().Int("concurrency", 10, "Parallel LLM requests")
	analyzeCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(analyzeCmd)

	scoreCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(scoreCmd)

	listCmd.Flags().Bool("json", false, "Output as JSON")
	listCmd.Flags().String("tier", "", "Filter by tier: critical, high, medium, low")
	listCmd.Flags().Int("limit", 0, "Max files to return (0 = all)")
	listCmd.Flags().String("sort", "heat", "Sort by: heat, likelihood, impact")
	listUnitsCmd.Flags().Bool("json", false, "Output as JSON")
	listUnitsCmd.Flags().Bool("include-supporting", false, "Include supporting units (examples, scripts, tests, helper bins under src/bin/)")
	listCmd.AddCommand(listUnitsCmd)
	rootCmd.AddCommand(listCmd)

	prCheckCmd.Flags().String("base", "main", "Base branch to compare against")
	prCheckCmd.Flags().Bool("json", false, "Output as JSON")
	prCheckCmd.Flags().String("fail-on", "", "Exit 5 when the PR tier is at or above this tier: critical, high, medium, low")
	prCheckCmd.Flags().Bool("shadow", false, "Log tier and decision to .heatmap/shadow.jsonl but always exit 0 (gate defaults to critical)")
	prCheckCmd.Flags().String("author", "", "PR author email or handle to exclude from reviewers (default: HEAD commit author)")
	prCheckCmd.Flags().Int("reviewers", 3, "Max suggested reviewers (0 disables ownership lookup)")
	prCheckCmd.Flags().String("format", "text", "Output format: text, markdown (the PR comment), json")
	prCheckCmd.Flags().String("repo", "", "GitHub owner/name the markdown comment links to for one-click issues and CODEOWNERS edits (default: $GITHUB_REPOSITORY, then the origin remote)")
	prCheckCmd.Flags().Int("pr", 0, "PR number the markdown comment's issues link back to (default: from $GITHUB_REF refs/pull/N/merge in GitHub Actions)")
	prCheckCmd.Flags().StringSlice("approvals", nil, "GitHub logins that approved the PR (CI fills it); ticks owner-review items when a suggested owner approved")
	prCmd.AddCommand(prCheckCmd)
	rootCmd.AddCommand(prCmd)

	incidentCreateCmd.Flags().String("file", "", "File path (required)")
	incidentCreateCmd.Flags().String("severity", "", "Severity: critical, high, medium, low (required)")
	incidentCreateCmd.Flags().String("description", "", "Description (required)")
	incidentCreateCmd.Flags().String("date", "", "Date (YYYY-MM-DD, defaults to today)")
	incidentCreateCmd.Flags().Bool("json", false, "Output as JSON")
	incidentListCmd.Flags().String("file", "", "Filter by file path")
	incidentListCmd.Flags().Bool("json", false, "Output as JSON")
	incidentCmd.AddCommand(incidentCreateCmd)
	incidentCmd.AddCommand(incidentListCmd)
	rootCmd.AddCommand(incidentCmd)

	reportCmd.Flags().Bool("json", false, "Output full heatmap as JSON")
	reportCmd.Flags().Int("limit", 10, "Max files per tier in text output")
	rootCmd.AddCommand(reportCmd)

	rootCmd.AddCommand(agentContextCmd)

	dashboardCmd.Flags().String("output", "", "Output path (default: .heatmap/dashboard.html)")
	dashboardCmd.Flags().Bool("no-open", false, "Don't open in browser")
	rootCmd.AddCommand(dashboardCmd)

	githubCmd.AddCommand(githubInstallCmd)
	rootCmd.AddCommand(githubCmd)
}

// headSHA is the short HEAD commit the comment's issues name, or "" outside git.
func headSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short=12", "HEAD").Output()
	if err != nil {
		return "" // optional context: the issues leave the commit out
	}
	return strings.TrimSpace(string(out))
}

// codeownersPath is the repo's CODEOWNERS file relative to the root, or "".
func codeownersPath() string {
	_, rel := policy.LoadCodeowners(".")
	return rel
}
