package main

import (
	"encoding/json"
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
	"github.com/zanetworker/highstakes/internal/prscore"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/tui"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

var version = "1.0.0"

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
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
	Short: "Get heat score and reasoning for a file",
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
			return printJSON(file)
		}

		printFileScore(file)
		return nil
	},
}

func printFileScore(file *types.FileHeat) {
	fmt.Printf("%s  %s %s  (score: %d)\n\n",
		file.Path, tierEmoji(file.Tier), strings.ToUpper(string(file.Tier)), file.HeatScore)

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

	req := file.ReviewRequirements
	fmt.Printf("\nReview: %d reviewers", req.MinReviewers)
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

// --- list ---

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List files by heat score",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		tier, _ := cmd.Flags().GetString("tier")
		limit, _ := cmd.Flags().GetInt("limit")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		// Validate tier flag
		validTiers := map[string]bool{"critical": true, "high": true, "medium": true, "low": true, "": true}
		if !validTiers[tier] {
			return cliError(2, "--tier must be one of: critical, high, medium, low (got: %q)", tier)
		}

		type entry struct {
			Path    string `json:"path"`
			Score   int    `json:"heat_score"`
			Tier    string `json:"tier"`
			Reason  string `json:"reason,omitempty"`
		}

		var files []entry
		for path, f := range hm.Files {
			if tier != "" && string(f.Tier) != tier {
				continue
			}
			reason := ""
			if f.Factors.BlastRadius.Assessed && f.Factors.BlastRadius.Summary != "" {
				reason = f.Factors.BlastRadius.Summary
			}
			files = append(files, entry{path, f.HeatScore, string(f.Tier), reason})
		}

		sort.Slice(files, func(i, j int) bool { return files[i].Score > files[j].Score })

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
			line := fmt.Sprintf("%s %3d  %-50s", emoji, f.Score, f.Path)
			if f.Reason != "" {
				line += "  " + f.Reason
			}
			fmt.Println(line)
		}

		return nil
	},
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

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		out, err := exec.Command("git", "diff", "--numstat", base+"...HEAD").Output()
		if err != nil {
			return cliError(3, "git diff failed against base %q: %v. Valid bases: main, origin/main, HEAD~N", base, err)
		}

		var changes []prscore.FileChange
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 3 {
				continue
			}
			added, _ := strconv.Atoi(parts[0])
			deleted, _ := strconv.Atoi(parts[1])
			changes = append(changes, prscore.FileChange{
				Path: parts[2], LinesAdded: added, LinesDeleted: deleted,
			})
		}

		if len(changes) == 0 {
			if jsonOut {
				return printJSON(map[string]interface{}{"changes": 0, "tier": "low"})
			}
			fmt.Println("No changes detected against", base)
			return nil
		}

		risk := prscore.CalculatePRRisk(0, changes, hm, scorer.DefaultTierThresholds())

		if jsonOut {
			return printJSON(risk)
		}

		sort.Slice(risk.FilesChanged, func(i, j int) bool {
			return risk.FilesChanged[i].HeatScore > risk.FilesChanged[j].HeatScore
		})

		fmt.Printf("PR Risk: %s %s (score: %d)\n\n", tierEmoji(risk.Tier), strings.ToUpper(string(risk.Tier)), risk.HeatScore)

		for _, fc := range risk.FilesChanged {
			icon := " "
			if fc.Tier == "critical" || fc.Tier == "high" {
				icon = "!"
			}
			reason := ""
			if f, ok := hm.Files[fc.Path]; ok && f.Factors.BlastRadius.Summary != "" {
				reason = "  " + f.Factors.BlastRadius.Summary
			}
			fmt.Printf(" %s %s %3d %-45s +%d/-%d%s\n",
				icon, tierEmoji(fc.Tier), fc.HeatScore, fc.Path,
				fc.LinesAdded, fc.LinesDeleted, reason)
		}

		if len(risk.CircuitBreakerSignals) > 0 {
			fmt.Println("\nCircuit Breakers:")
			for _, s := range risk.CircuitBreakerSignals {
				fmt.Printf("  - %s\n", s)
			}
		}

		req := risk.ReviewRequirements
		fmt.Printf("\nReview: %d reviewers", req.MinReviewers)
		if req.RequiresSenior {
			fmt.Print(" (senior)")
		}
		if req.RequiresSecurityScan {
			fmt.Print(", security scan")
		}
		fmt.Printf(", ~%d min\n", req.EstimatedReviewTimeMinutes)
		return nil
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

		if err := dashboard.Generate(hm, output); err != nil {
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
		ctx := map[string]interface{}{
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
						"--model":       flagDef("string", "deepseek/deepseek-v4-flash", "OpenRouter model ID"),
						"--concurrency": flagDef("int", 10, "Parallel LLM requests"),
					},
				},
				"get": map[string]interface{}{
					"description": "Get heat score and blast radius reasoning for a file",
					"args":        "file_path (required)",
					"flags":       map[string]interface{}{"--json": flagDef("bool", false, "Output as JSON")},
				},
				"list": map[string]interface{}{
					"description": "List files sorted by heat score",
					"flags": map[string]interface{}{
						"--json":  flagDef("bool", false, "Output as JSON"),
						"--tier":  flagDef("enum", "", "Filter by tier. Values: critical, high, medium, low"),
						"--limit": flagDef("int", 0, "Max files to return (0 = all)"),
					},
				},
				"pr": map[string]interface{}{
					"description": "Assess pull request risk",
					"subcommands": map[string]interface{}{
						"check": map[string]interface{}{
							"description": "Score current diff against a base branch",
							"flags": map[string]interface{}{
								"--json": flagDef("bool", false, "Output as JSON"),
								"--base": flagDef("string", "main", "Base branch"),
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
							"description": "Install GitHub Action for automated PR triage",
						},
					},
				},
			},
			"environment": map[string]interface{}{
				"OPENROUTER_API_KEY": "Required for LLM blast radius analysis. Get from openrouter.ai",
			},
			"models": []string{
				"deepseek/deepseek-v4-flash (default, cheapest)",
				"deepseek/deepseek-v4-pro (best accuracy/cost)",
				"z-ai/glm-5.2 (frontier open-weights)",
				"openai/gpt-5.4-mini (safest JSON)",
				"google/gemini-3-flash (fastest)",
				"anthropic/claude-haiku-4.5 (best reasoning)",
			},
			"exit_codes": map[string]interface{}{
				"0": "Success",
				"1": "Internal error",
				"2": "Invalid input (bad flags, missing required params)",
				"3": "External dependency failure (git, API)",
				"4": "Not found (file not in heatmap)",
			},
		}
		return printJSON(ctx)
	},
}

// --- github ---

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

		workflow := `name: Code Heatmap Triage
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: read

jobs:
  triage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Install heatmap
        run: go install github.com/zanetworker/highstakes/cmd/heatmap@latest

      - name: Analyze
        run: highstakes init && highstakes analyze --json
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}

      - name: Check PR
        run: highstakes pr check --base origin/${{ github.base_ref }} --json > pr-risk.json

      - name: Comment
        uses: actions/github-script@v7
        with:
          script: |
            const risk = JSON.parse(require('fs').readFileSync('pr-risk.json','utf8'));
            const e = {critical:'🔥🔥🔥',high:'🔥🔥',medium:'🔥',low:'🟢'};
            let b = '## '+e[risk.tier]+' Code Heatmap: '+risk.tier.toUpperCase()+'\n\n';
            b += '| File | Heat | Tier | Lines |\n|------|------|------|-------|\n';
            for (const f of risk.files_changed.sort((a,b)=>b.heat_score-a.heat_score))
              b += '| '+(f.tier==='critical'||f.tier==='high'?'⚠️':'✓')+' '+f.path+' | '+f.heat_score+' | '+e[f.tier]+' | +'+f.lines_added+'/-'+f.lines_deleted+' |\n';
            if (risk.circuit_breaker_signals?.length) {
              b += '\n### Circuit Breakers\n';
              risk.circuit_breaker_signals.forEach(s => b += '- '+s+'\n');
            }
            b += '\n**Review:** '+risk.review_requirements.min_reviewers+' reviewers';
            if (risk.review_requirements.requires_senior) b += ' (senior)';
            b += ', ~'+risk.review_requirements.estimated_review_time_minutes+' min';
            b += ', auto-merge: '+(risk.review_requirements.auto_merge?'✅':'❌')+'\n';
            const comments = await github.rest.issues.listComments({owner:context.repo.owner,repo:context.repo.repo,issue_number:context.issue.number});
            const existing = comments.data.find(c=>c.body.includes('Code Heatmap:'));
            const method = existing ? 'updateComment' : 'createComment';
            const params = {owner:context.repo.owner,repo:context.repo.repo,...(existing?{comment_id:existing.id}:{issue_number:context.issue.number}),body:b};
            await github.rest.issues[method](params);
`

		workflowPath := filepath.Join(workflowDir, "heatmap-triage.yml")
		if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
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

// --- command registration ---

func init() {
	// Structured exit codes
	cobra.AddTemplateFunc("exit", func() {})
	rootCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) {}

	initCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(initCmd)

	analyzeCmd.Flags().Bool("no-llm", false, "Skip LLM analysis, use static heuristics only")
	analyzeCmd.Flags().Bool("force", false, "Re-assess all files (ignore cache)")
	analyzeCmd.Flags().String("model", "", "OpenRouter model (default: deepseek/deepseek-v4-flash)")
	analyzeCmd.Flags().Int("concurrency", 10, "Parallel LLM requests")
	analyzeCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(analyzeCmd)

	scoreCmd.Flags().Bool("json", false, "Output as JSON")
	rootCmd.AddCommand(scoreCmd)

	listCmd.Flags().Bool("json", false, "Output as JSON")
	listCmd.Flags().String("tier", "", "Filter by tier: critical, high, medium, low")
	listCmd.Flags().Int("limit", 0, "Max files to return (0 = all)")
	rootCmd.AddCommand(listCmd)

	prCheckCmd.Flags().String("base", "main", "Base branch to compare against")
	prCheckCmd.Flags().Bool("json", false, "Output as JSON")
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
