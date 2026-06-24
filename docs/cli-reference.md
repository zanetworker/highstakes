# CLI Reference

All commands support `--json` for machine-readable output.

## heatmap

Launch the interactive TUI explorer.

```sh
heatmap
```

Keyboard: `↑↓` navigate, `Enter` expand, `f` filter by tier, `s` sort, `/` search, `q` quit.

## highstakes init

Initialize heatmap in the current repository.

```sh
highstakes init [--json]
```

Creates `.heatmap/` directory and `.heatmap/config.yaml` with default settings.

## highstakes analyze

Analyze the repository and generate the heatmap.

```sh
highstakes analyze [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--no-llm` | `false` | Skip LLM analysis, use static heuristics only |
| `--force` | `false` | Re-assess all files, ignoring cache |
| `--model` | `deepseek/deepseek-v4-flash` | OpenRouter model identifier |
| `--concurrency` | `10` | Number of parallel LLM requests |
| `--json` | `false` | Output summary as JSON |

## heatmap get

Get the heat score and blast radius reasoning for a specific file.

```sh
highstakes get <file> [--json]
```

If the file is not found, suggests similar paths from the heatmap.

**Exit code 4** if file not found.

## highstakes list

List files sorted by heat score.

```sh
highstakes list [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tier` | (all) | Filter by tier: `critical`, `high`, `medium`, `low` |
| `--limit` | `0` | Maximum files to return (0 = all) |
| `--json` | `false` | Output as JSON |

**Exit code 2** if `--tier` value is invalid (error includes valid values).

## highstakes pr check

Assess risk of the current diff against a base branch.

```sh
highstakes pr check [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--base` | `main` | Base branch to compare against |
| `--json` | `false` | Output as JSON |

Uses `git diff --numstat` to identify changed files, then looks up their heat scores. The PR inherits the tier of its highest-scoring file.

**Exit code 3** if git diff fails (error includes valid base examples).

## highstakes incident create

Record a production incident for a file.

```sh
highstakes incident create --file <path> --severity <level> --description <text> [--date YYYY-MM-DD] [--json]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--file` | Yes | File path |
| `--severity` | Yes | `critical`, `high`, `medium`, or `low` |
| `--description` | Yes | What happened |
| `--date` | No | Incident date (default: today) |

Incidents are stored in `.heatmap/incidents.json` and feed into the heat score on next `highstakes analyze`.

## highstakes incident list

List recorded incidents.

```sh
highstakes incident list [--file <path>] [--json]
```

## highstakes report

Generate a heat distribution report.

```sh
highstakes report [--limit N] [--json]
```

Text output shows files grouped by tier with blast radius reasoning. JSON output returns the full heatmap data.

## highstakes dashboard

Generate an interactive HTML dashboard and open it in the browser.

```sh
highstakes dashboard [--output <path>] [--no-open]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `.heatmap/dashboard.html` | Output file path |
| `--no-open` | `false` | Don't open in browser |

The dashboard has two views: a treemap (visual overview) and a file explorer (hierarchical tree). Both support tier filtering, search, and a detail panel with blast radius reasoning.

## highstakes github install

Generate a GitHub Actions workflow for automated PR triage.

```sh
highstakes github install
```

Creates `.github/workflows/heatmap-triage.yml`.

## highstakes agent-context

Output machine-readable JSON describing all commands, flags, exit codes, and available models.

```sh
highstakes agent-context
```

Designed for AI agents to discover the CLI surface programmatically. The output includes a `schema_version` field for detecting breaking changes.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Internal error |
| `2` | Invalid input (bad flags, missing required params) |
| `3` | External dependency failure (git, API) |
| `4` | Not found (file not in heatmap) |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENROUTER_API_KEY` | OpenRouter API key (default provider) |
| `HIGHSTAKES_API_KEY` | API key for any OpenAI-compatible endpoint (overrides `OPENROUTER_API_KEY`) |
| `HIGHSTAKES_API_URL` | Base URL for the API endpoint (used with `HIGHSTAKES_API_KEY`) |

Set either `OPENROUTER_API_KEY` or `HIGHSTAKES_API_KEY` + `HIGHSTAKES_API_URL`. Neither is needed with `--no-llm`.
