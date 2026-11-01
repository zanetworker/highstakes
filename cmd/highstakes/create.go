package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/policy"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
	"gopkg.in/yaml.v3"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create review artifacts (CODEOWNERS block, AI reviewer policy, maintainer labelling sheet) from the heatmap",
}

var createCodeownersCmd = &cobra.Command{
	Use:   "codeowners",
	Short: "Generate a CODEOWNERS block for high-stakes paths with owners from git history",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		write, _ := cmd.Flags().GetBool("write")
		file, _ := cmd.Flags().GetString("file")
		maxOwners, _ := cmd.Flags().GetInt("max-owners")
		minTierStr, _ := cmd.Flags().GetString("min-tier")

		minTier, err := policy.ParseTier(minTierStr)
		if err != nil {
			return cliError(2, "--min-tier: %v", err)
		}
		if maxOwners < 1 {
			return cliError(2, "--max-owners must be >= 1, got %d", maxOwners)
		}
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		cfg, err := loadConfigOrDefault()
		if err != nil {
			return err
		}

		entries := policy.HotPaths(hm, minTier, cfg.Tiers)
		var files []string
		for _, e := range entries {
			files = append(files, e.Files...)
		}
		owners := map[string][]ownership.Owner{}
		if len(files) > 0 {
			owners, err = ownership.FileOwners(".", files, ownership.Options{Aliases: cfg.Owners})
			if err != nil {
				return cliError(3, "read git history for owners: %v", err)
			}
		}
		block, rows := policy.Codeowners(entries, owners, maxOwners)

		written := ""
		if write {
			existing, err := os.ReadFile(file)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return cliError(1, "read %s: %v", file, err)
			}
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				return cliError(1, "create directory for %s: %v", file, err)
			}
			if err := os.WriteFile(file, []byte(policy.ReplaceBlock(string(existing), block)), 0o644); err != nil {
				return cliError(1, "write %s: %v", file, err)
			}
			written = file
		}

		if jsonOut {
			return printJSON(map[string]interface{}{
				"min_tier": minTier,
				"entries":  rows,
				"block":    block,
				"written":  written,
			})
		}
		fmt.Print(block)
		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, noFilesHint(string(minTier)))
		}
		if written != "" {
			fmt.Fprintf(os.Stderr, "Updated %s (%d paths)\n", written, len(rows))
		}
		return nil
	},
}

var createPolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Generate a no-auto-approve path policy (YAML, or JSON with --json) for AI reviewers",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		minTierStr, _ := cmd.Flags().GetString("min-tier")

		minTier, err := policy.ParseTier(minTierStr)
		if err != nil {
			return cliError(2, "--min-tier: %v", err)
		}
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		cfg, err := loadConfigOrDefault()
		if err != nil {
			return err
		}
		p := policy.Build(hm, policy.HotPaths(hm, minTier, cfg.Tiers), minTier)
		if jsonOut {
			return printJSON(p)
		}
		out, err := yaml.Marshal(p)
		if err != nil {
			return cliError(1, "encode policy as YAML: %v", err)
		}
		fmt.Print(string(out))
		return nil
	},
}

// loadConfigOrDefault reads .heatmap/config.yaml, falling back to defaults when absent.
func loadConfigOrDefault() (types.Config, error) {
	path := filepath.Join(".heatmap", "config.yaml")
	cfg, err := heatmap.LoadConfig(path)
	if errors.Is(err, fs.ErrNotExist) {
		return heatmap.DefaultConfig(), nil
	}
	if err != nil {
		return types.Config{}, cliError(2, "invalid config %s: %v", path, err)
	}
	return cfg, nil
}

func init() {
	createCodeownersCmd.Flags().Bool("json", false, "Output as JSON")
	createCodeownersCmd.Flags().Bool("write", false, "Write the block into --file (replaces the marked block, or appends it)")
	createCodeownersCmd.Flags().String("file", filepath.Join(".github", "CODEOWNERS"), "CODEOWNERS file used by --write")
	createCodeownersCmd.Flags().String("min-tier", "high", "Lowest tier to include: critical, high, medium, low")
	createCodeownersCmd.Flags().Int("max-owners", 2, "Owners per path")
	createCmd.AddCommand(createCodeownersCmd)

	createPolicyCmd.Flags().Bool("json", false, "Output as JSON instead of YAML")
	createPolicyCmd.Flags().String("min-tier", "high", "Lowest tier to include: critical, high, medium, low")
	createCmd.AddCommand(createPolicyCmd)

	rootCmd.AddCommand(createCmd)
}

// noFilesHint suggests the next lower tier when nothing meets --min-tier.
func noFilesHint(minTier string) string {
	lower := map[string]string{"critical": "high", "high": "medium", "medium": "low"}[minTier]
	if lower == "" {
		return fmt.Sprintf("No files at or above tier %s. Run analyze first, with an LLM for meaningful tiers.", minTier)
	}
	return fmt.Sprintf("No files at or above tier %s. Try --min-tier %s, or run analyze with an LLM.", minTier, lower)
}
