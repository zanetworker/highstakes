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
cmd/highstakes/create.go       `create codeowners` / `create policy`
internal/
  analyzer/                  Static analysis (AST, imports, complexity)
  gitanalyzer/               Git history (commits, churn, contributors)
  llm/                       LLM blast radius assessment via OpenRouter
  scorer/                    Likelihood, impact, heat and quadrant tier assignment
  prscore/                   PR risk scoring (importance + change risk), reviewers, gate
  ownership/                 Per-file owners from git log (recency-weighted, .mailmap aware)
  policy/                    Hot path collapsing, CODEOWNERS block, AI reviewer policy
  incidents/                 Incident tracking persistence
  tui/                       Bubbletea TUI (tree model + view)
  dashboard/                 HTML dashboard generator (treemap + explorer)
  types/                     Shared type definitions
pkg/heatmap/                 Public API (generator, config, save/load)
docs/                        User-facing documentation
```

## Key Patterns

- Scoring: two axes per file, likelihood (churn, complexity, centrality, incidents; no LLM) and impact (LLM blast radius); tier from the likelihood x impact quadrant (scorer.ApplyQuadrants), heat_score = max of the two for compatibility
- LLM calls go through OpenRouter (OpenAI-compatible format), default model is GPT-6 Luna (chosen on the bench/ benchmark)
- Assessments cached in .heatmap/cache/ by SHA-256 of model ID, prompt version (llm.PromptVersion) and file content
- Analyzer excludes configurable in .heatmap/config.yaml (not hardcoded)
- All CLI commands support --json for machine-readable output
- Dashboard is self-contained HTML embedded as a Go string constant

## Conventions

- CLI verbs follow agentic CLI vocabulary: get, list, create, check (not show, find, add)
- Errors include valid values when an enum is the cause
- Exit codes: 0=success, 1=internal, 2=invalid input, 3=external failure, 4=not found, 5=PR blocked by `pr check --fail-on`
- Tests use table-driven patterns with t.Run subtests
