package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/explain"
	"github.com/zanetworker/highstakes/internal/types"
)

var listTasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List backlog items agents may take on, as self-contained tasks",
	Long: `List backlog items on files at or above an agent autonomy level (default 2:
what agents may do unattended or with one approval). Each task names the
file, the signal that triggered it, the command if any, and when it counts
as done. Levels: 3 auto-merge, 2 auto-fix with a human merging, 1 propose
only, 0 assist only; autonomy.max_level in .heatmap/config.yaml caps them.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		level, _ := cmd.Flags().GetInt("level")
		if level < 0 || level > types.MaxAutonomy {
			return cliError(2, "--level must be one of: 0, 1, 2, 3 (got: %d)", level)
		}
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		cfg, err := loadConfigOrDefault()
		if err != nil {
			return err
		}
		maxLevel, _ := cfg.Autonomy.Max() // validated when the config was loaded
		tasks := explain.Tasks(hm, level, maxLevel)
		if jsonOut {
			if tasks == nil {
				tasks = []explain.Task{}
			}
			return printJSON(map[string]interface{}{"tasks": tasks, "count": len(tasks), "min_level": level, "max_level": maxLevel})
		}
		if len(tasks) == 0 {
			fmt.Printf("No backlog items on files at L%d or above.", level)
			if level > 0 {
				fmt.Printf(" Try --level %d, or run analyze with an LLM (files without an assessed impact stay at L1 or below).", level-1)
			}
			fmt.Println()
			return nil
		}
		for _, t := range tasks {
			fmt.Printf("L%d %s  %s\n", t.Level, t.LevelLabel, t.Title)
			fmt.Printf("    why: %s\n    done when: %s\n", t.Signal, t.Acceptance)
			if t.Command != "" {
				fmt.Printf("    $ %s\n", t.Command)
			}
		}
		return nil
	},
}

func init() {
	listTasksCmd.Flags().Bool("json", false, "Output as JSON")
	listTasksCmd.Flags().Int("level", 2, "Lowest autonomy level to include: 0, 1, 2, 3")
	listCmd.AddCommand(listTasksCmd)
}
