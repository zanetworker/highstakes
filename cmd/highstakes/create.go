package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
		block, rows, unrepresentable, unmapped := policy.Codeowners(entries, owners, maxOwners)

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
				"min_tier":        minTier,
				"entries":         rows,
				"unrepresentable": unrepresentable,
				"unmapped_owners": unmapped,
				"block":           block,
				"written":         written,
			})
		}
		fmt.Print(block)
		if len(rows) == 0 && len(unrepresentable) == 0 {
			fmt.Fprintln(os.Stderr, noFilesHint(string(minTier)))
		}
		if len(unrepresentable) > 0 {
			fmt.Fprintf(os.Stderr, "%d hot paths contain a space or glob character CODEOWNERS cannot express; left as comments\n", len(unrepresentable))
		}
		if len(unmapped) > 0 {
			fmt.Fprintf(os.Stderr, "%d owners have no GitHub handle and were left out (emails are never written): map them under owners: in .heatmap/config.yaml, e.g. %q: \"@handle\"\n  %s\n",
				len(unmapped), unmapped[0], strings.Join(unmapped, "\n  "))
		}
		if written != "" {
			fmt.Fprintf(os.Stderr, "Updated %s (%d paths)\n", written, len(rows))
		}
		return nil
	},
}

var createPolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Generate a no-auto-approve path policy for AI reviewers, or with --agents an autonomy policy for AI agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		minTierStr, _ := cmd.Flags().GetString("min-tier")
		agents, _ := cmd.Flags().GetBool("agents")
		format, _ := cmd.Flags().GetString("format")
		outPath, _ := cmd.Flags().GetString("out")

		if jsonOut {
			format = "json"
		}
		if format != "yaml" && format != "json" {
			return cliError(2, "--format must be one of: yaml, json (got: %q)", format)
		}
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
		var p interface{} = policy.Build(hm, policy.HotPaths(hm, minTier, cfg.Tiers), minTier)
		header := ""
		if agents {
			maxLevel, _ := cfg.Autonomy.Max() // validated when the config was loaded
			p, header = policy.Agents(hm, maxLevel), agentPolicyHeader
		}
		var out []byte
		if format == "json" {
			out, err = json.MarshalIndent(p, "", "  ")
			out = append(out, '\n')
		} else {
			out, err = yaml.Marshal(p)
			out = append([]byte(header), out...)
		}
		if err != nil {
			return cliError(1, "encode policy as %s: %v", format, err)
		}
		if outPath == "" {
			fmt.Print(string(out))
			return nil
		}
		if dir := filepath.Dir(outPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return cliError(1, "create directory for %s: %v", outPath, err)
			}
		}
		if err := os.WriteFile(outPath, out, 0o644); err != nil {
			return cliError(1, "write %s: %v", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", outPath)
		return nil
	},
}

// agentPolicyHeader opens the YAML autonomy policy for people skimming it;
// how_to_read and cap_rule carry the same rules for bots and JSON readers.
const agentPolicyHeader = `# Agent autonomy policy, generated by ` + "`highstakes create policy --agents`" + `.
# Levels: 3 auto-merge (agents fix, open a PR, merge once CI and AI review pass),
# 2 auto-fix (a human merges), 1 propose only (draft PRs or suggestions; an owner
# merges), 0 assist only (tests and explanations; changes need a named senior owner).
# A change may go as far as the lowest level among the files it touches.
# Any path not listed is L1 (propose only) by default, L0 if max_level is 0;
# levels 2 and 3 list exact files, so new files never inherit them.
# Levels are conservative: impact is AI-assessed, files without an LLM assessment
# are capped at 1, and autonomy.max_level in .heatmap/config.yaml caps everything.
`

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
	createPolicyCmd.Flags().Bool("agents", false, "Emit the agent autonomy policy (path globs per level 0-3) instead")
	createPolicyCmd.Flags().String("format", "yaml", "Output format: yaml, json")
	createPolicyCmd.Flags().String("out", "", "Write to this file instead of stdout (e.g. .highstakes/autonomy.yaml)")
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
