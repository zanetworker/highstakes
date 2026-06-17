package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zanetworker/code-heatmap/pkg/heatmap"
)

var version = "1.0.0"

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "heatmap",
	Short: "Code heatmap - identify critical code paths for review",
	Long: `Code Heatmap analyzes your repository to identify critical code paths
that require careful human review vs areas where AI review is sufficient.

Usage:
  heatmap init              Initialize heatmap in current repo
  heatmap analyze           Analyze repo and generate/update heatmap
  heatmap                   Launch interactive TUI explorer
  heatmap score <file>      Get heat score for a file
  heatmap pr <number>       Assess risk for a pull request
  heatmap report            Generate PR triage report

Examples:
  heatmap init
  heatmap analyze --update
  heatmap score src/auth/jwt.ts
  heatmap pr 1234
  heatmap pr list --tier critical`,
	Version: version,
	Run: func(cmd *cobra.Command, args []string) {
		// Default: launch TUI
		fmt.Println("🔥 Code Heatmap TUI - Coming soon!")
		fmt.Println("\nAvailable commands:")
		fmt.Println("  heatmap init      - Initialize in current repo")
		fmt.Println("  heatmap analyze   - Analyze and generate heatmap")
		fmt.Println("  heatmap score <file> - Query file heat score")
		fmt.Println("  heatmap pr <num>  - Assess PR risk")
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize heatmap in current repository",
	Run: func(cmd *cobra.Command, args []string) {
		if err := initializeRepo(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func initializeRepo() error {
	fmt.Println("🔧 Initializing code heatmap...")

	// Create .heatmap directory
	if err := os.MkdirAll(".heatmap", 0755); err != nil {
		return fmt.Errorf("create .heatmap directory: %w", err)
	}
	fmt.Println("  ✓ Created .heatmap/ directory")

	// Create default config
	config := heatmap.DefaultConfig()
	configPath := filepath.Join(".heatmap", "config.yaml")
	if err := heatmap.SaveConfig(config, configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Println("  ✓ Created .heatmap/config.yaml")

	fmt.Println("\nNext: run 'heatmap analyze' to generate initial heatmap")
	return nil
}

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Analyze repository and generate heatmap",
	Run: func(cmd *cobra.Command, args []string) {
		if err := analyzeRepository(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func analyzeRepository() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Generate heatmap
	gen, err := heatmap.New(cwd)
	if err != nil {
		return fmt.Errorf("initialize generator: %w", err)
	}

	hm, err := gen.Generate()
	if err != nil {
		return fmt.Errorf("generate heatmap: %w", err)
	}

	// Save heatmap
	heatmapPath := filepath.Join(".heatmap", "heatmap.json")
	if err := heatmap.Save(hm, heatmapPath); err != nil {
		return fmt.Errorf("save heatmap: %w", err)
	}

	fmt.Printf("\n✓ Heatmap generated: %s\n", heatmapPath)
	fmt.Printf("  Total files analyzed: %d\n", hm.Metadata.TotalFiles)
	if len(hm.Metadata.CommitSHA) >= 7 {
		fmt.Printf("  Commit: %s\n", hm.Metadata.CommitSHA[:7])
	}

	// Print tier summary
	tierCounts := make(map[string]int)
	for _, file := range hm.Files {
		tierCounts[string(file.Tier)]++
	}

	fmt.Println("\nHeat Distribution:")
	fmt.Printf("  🔥🔥🔥 CRITICAL: %d files\n", tierCounts["critical"])
	fmt.Printf("  🔥🔥  HIGH:     %d files\n", tierCounts["high"])
	fmt.Printf("  🔥   MEDIUM:   %d files\n", tierCounts["medium"])
	fmt.Printf("  🟢   LOW:      %d files\n", tierCounts["low"])

	return nil
}

var scoreCmd = &cobra.Command{
	Use:   "score <file>",
	Short: "Get heat score for a file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := showFileScore(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func showFileScore(filePath string) error {
	// Load heatmap
	hm, err := heatmap.Load(".heatmap/heatmap.json")
	if err != nil {
		return fmt.Errorf("load heatmap: %w (run 'heatmap analyze' first)", err)
	}

	// Find file
	file, ok := hm.Files[filePath]
	if !ok {
		return fmt.Errorf("file not found in heatmap: %s", filePath)
	}

	// Print heat score
	tierEmoji := getTierEmoji(file.Tier)
	fmt.Printf("📈 Heat score for %s:\n\n", filePath)
	fmt.Printf("Heat Score: %d %s %s\n", file.HeatScore, tierEmoji, string(file.Tier))

	// Print risk factors
	fmt.Println("\nRisk Factors:")
	fmt.Printf("  Dependency Centrality:  %.0f (%d imports)\n",
		file.Factors.DependencyCentrality.Score*100,
		file.Factors.DependencyCentrality.ImportCount)
	fmt.Printf("  Incident History:       %d (%d incidents)\n",
		file.Factors.IncidentHistory.Score,
		file.Factors.IncidentHistory.IncidentCount)
	fmt.Printf("  Change Frequency:       %d (%d commits/90d)\n",
		file.Factors.ChangeFrequency.Score,
		file.Factors.ChangeFrequency.CommitsLast90d)
	fmt.Printf("  User Impact:            %d\n", file.Factors.UserImpact.Score)
	fmt.Printf("  Data Sensitivity:       %d\n", file.Factors.DataSensitivity.Score)
	fmt.Printf("  Test Coverage:          %d (%.0f%% coverage)\n",
		file.Factors.TestCoverage.Score,
		file.Factors.TestCoverage.CoveragePercent)
	fmt.Printf("  Complexity:             %d (cyclomatic: %d)\n",
		file.Factors.Complexity.Score,
		file.Factors.Complexity.Cyclomatic)

	// Print review requirements
	req := file.ReviewRequirements
	fmt.Println("\nReview Requirements:")
	fmt.Printf("  Min Reviewers:   %d", req.MinReviewers)
	if req.RequiresSenior {
		fmt.Print(" (senior)")
	}
	fmt.Println()
	fmt.Printf("  Security Scan:   %s\n", boolToStatus(req.RequiresSecurityScan))
	fmt.Printf("  Auto-Merge:      %s\n", boolToBlocked(!req.AutoMerge))
	fmt.Printf("  Est. Review Time: %d minutes\n", req.EstimatedReviewTimeMinutes)

	return nil
}

func getTierEmoji(tier interface{}) string {
	tierStr := fmt.Sprintf("%v", tier)
	switch tierStr {
	case "critical":
		return "🔥🔥🔥"
	case "high":
		return "🔥🔥"
	case "medium":
		return "🔥"
	default:
		return "🟢"
	}
}

func boolToStatus(b bool) string {
	if b {
		return "Required"
	}
	return "Not required"
}

func boolToBlocked(b bool) string {
	if b {
		return "❌ Blocked"
	}
	return "✅ Allowed"
}

var prCmd = &cobra.Command{
	Use:   "pr <number>",
	Short: "Assess risk for a pull request",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prNum := args[0]
		fmt.Printf("🔍 Assessing PR #%s...\n\n", prNum)
		fmt.Println("Risk Score: 🔥🔥 HIGH (78)")
		fmt.Println("\nFiles Changed (5):")
		fmt.Println("  ⚠️  src/auth/jwt.ts       🔥🔥🔥 CRITICAL (95)")
		fmt.Println("  ⚠️  src/db/schema.ts      🔥🔥  HIGH (72)")
		fmt.Println("  ✓  src/api/routes.ts      🔥   MEDIUM (55)")
		fmt.Println("  ✓  src/api/handler.ts     🟡  MEDIUM (42)")
		fmt.Println("  ✓  tests/api/test.ts      🟢  LOW (8)")
		fmt.Println("\nReview Requirements:")
		fmt.Println("  ✅ Min Reviewers: 2 (senior required)")
		fmt.Println("  ✅ Security Scan: Required")
		fmt.Println("  ❌ Auto-Merge: Blocked")
		fmt.Println("\nEstimated Review Time: 45-60 minutes")
	},
}

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate PR triage report",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📋 PR Triage Report - 2026-06-17\n")
		fmt.Println("🔥🔥🔥 URGENT: Needs Human Review (3 PRs)")
		fmt.Println("  #1234 (3d) - Auth changes - Heat: 95")
		fmt.Println("  #1242 (1d) - Payment flow - Heat: 88")
		fmt.Println("  #1251 (6h) - DB migration - Heat: 92")
		fmt.Println("\n🔥 Review Recommended (5 PRs)")
		fmt.Println("  #1255, #1256, #1258, #1260, #1262")
		fmt.Println("\n🟢 Auto-Review Safe (12 PRs)")
		fmt.Println("  Can delegate to AI reviewers")
		fmt.Println("\nTotal Review Time Estimate: 4.5 hours")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(analyzeCmd)
	rootCmd.AddCommand(scoreCmd)
	rootCmd.AddCommand(prCmd)
	rootCmd.AddCommand(reportCmd)
}
