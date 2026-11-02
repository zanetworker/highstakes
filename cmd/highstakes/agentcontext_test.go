package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestAgentContext_CoversEveryCommandAndFlag walks rootCmd so a new command
// or flag cannot ship without its agent-context entry.
func TestAgentContext_CoversEveryCommandAndFlag(t *testing.T) {
	cmds, _ := agentContext()["commands"].(map[string]interface{})
	if len(cmds) == 0 {
		t.Fatal("agent-context has no commands")
	}
	var walk func(c *cobra.Command, entries map[string]interface{}, path string)
	walk = func(c *cobra.Command, entries map[string]interface{}, path string) {
		for _, sub := range c.Commands() {
			name := sub.Name()
			if sub.Hidden || name == "help" || name == "completion" {
				continue
			}
			p := path + " " + name
			entry, ok := entries[name].(map[string]interface{})
			if !ok {
				t.Errorf("agent-context is missing command %q", p)
				continue
			}
			flags, _ := entry["flags"].(map[string]interface{})
			sub.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" || f.Hidden {
					return
				}
				if _, ok := flags["--"+f.Name]; !ok {
					t.Errorf("agent-context %q is missing flag --%s", p, f.Name)
				}
			})
			subs, _ := entry["subcommands"].(map[string]interface{})
			walk(sub, subs, p)
		}
	}
	walk(rootCmd, cmds, "highstakes")
}
