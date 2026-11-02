# Code Heatmap

Go CLI tool that identifies critical code paths using LLM blast radius analysis.

## Build and Test

```sh
go build -o highstakes ./cmd/highstakes
go test ./... -count=1
go install ./cmd/highstakes
```

## Project Structure

```
cmd/highstakes/main.go         CLI entry point (Cobra)
cmd/highstakes/create.go       `create codeowners` / `create policy` (`--agents`: autonomy policy)
cmd/highstakes/tasks.go        `list tasks` (backlog as an agent work queue)
internal/
  analyzer/                  Static analysis (AST, imports, complexity)
  gitanalyzer/               Git history (commits, churn, contributors)
  llm/                       LLM blast radius assessment via OpenRouter
  scorer/                    Likelihood, impact, heat and quadrant tier assignment
  prscore/                   PR risk scoring (importance + change risk), reviewers, gate
  prstate/                   PR sign-off, live state and freshness checks for CI (pr signoff/state/fresh; gh behind an API interface)
  ownership/                 Per-file owners from git log (recency-weighted, .mailmap aware)
  policy/                    Hot path collapsing, CODEOWNERS block and coverage, AI reviewer policy
  explain/                   Verdict, reasons and scoped actions per file; PR-time done marking (pure, no LLM)
  prcomment/                 Markdown PR comment for `pr check --format markdown` (golden-file tested)
  incidents/                 Incident tracking persistence
  tui/                       Bubbletea TUI (tree model + view)
  dashboard/                 HTML dashboard generator (action inbox default; treemap, explorer, architecture, quadrant tabs)
  types/                     Shared type definitions
pkg/heatmap/                 Public API (generator, config, save/load)
docs/                        User-facing documentation
```

## Key Patterns

- Human-facing names: likelihood = "Will it break?", impact = "How bad if it breaks?"; quadrants = postures Harden now / Protect / Stabilize / Routine (types.Quadrant.Label). JSON keeps `quadrant` values and adds `quadrant_label`; tiers stay as metadata and filters. Never print a score without its repo cutoff
- Actions have a scope (before_merge | backlog), a signal, and backlog items a copyable command; rules live in internal/explain/actions.go, PR-time marking in internal/explain/pr.go
- Agent autonomy: types.Quadrant.Autonomy() gives the level (Routine 3 ... Harden now 0); scorer.Autonomy/FileAutonomy apply the caps (L1 when impact is not LLM-assessed, autonomy.max_level); prscore.ApplyAutonomy does the PR's level
- Scoring: two axes per file, likelihood (churn, complexity, centrality, incidents; no LLM) and impact (LLM blast radius); tier from the likelihood x impact quadrant (scorer.ApplyQuadrants), heat_score (kept for compatibility) = max of the two when impact is assessed, else the likelihood
- LLM calls go through OpenRouter (OpenAI-compatible format), default model is GPT-6 Luna (chosen on the bench/ benchmark)
- Assessments cached in .heatmap/cache/ by SHA-256 of model ID, prompt version (llm.PromptVersion) and file content
- Analyzer excludes configurable in .heatmap/config.yaml (not hardcoded)
- All CLI commands support --json for machine-readable output
- Dashboard is self-contained HTML embedded as a Go string constant

## Conventions

- CLI verbs follow agentic CLI vocabulary: get, list, create, check (not show, find, add)
- Errors include valid values when an enum is the cause
- Exit codes: 0=success, 1=internal, 2=invalid input, 3=external failure, 4=not found, 5=PR blocked by `pr check --fail-on`, or changed during the run (`pr fresh`)
- Tests use table-driven patterns with t.Run subtests
