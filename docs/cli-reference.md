# CLI Reference

All commands support `--json` for machine-readable output.

## heatmap

Launch the interactive TUI explorer.

```sh
heatmap
```

Keyboard: `↑↓` navigate, `Enter` expand, `f` filter by tier, `s` sort, `/` search, `q` quit.

## heatmap init

Initialize heatmap in the current repository.

```sh
heatmap init [--json]
```

Creates `.heatmap/` directory and `.heatmap/config.yaml` with default settings.

## heatmap analyze

Analyze the repository and generate the heatmap.

```sh
heatmap analyze [flags]
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
heatmap get <file> [--json]
```

If the file is not found, suggests similar paths from the heatmap.

**Exit code 4** if file not found.

## heatmap list

List files sorted by heat score.

```sh
heatmap list [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tier` | (all) | Filter by tier: `critical`, `high`, `medium`, `low` |
| `--limit` | `0` | Maximum files to return (0 = all) |
| `--json` | `false` | Output as JSON |

**Exit code 2** if `--tier` value is invalid (error includes valid values).

## heatmap pr check

Assess risk of the current diff against a base branch.

```sh
heatmap pr check [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--base` | `main` | Base branch to compare against |
| `--json` | `false` | Output as JSON |

Uses `git diff --numstat` to identify changed files, then looks up their heat scores. The PR inherits the tier of its highest-scoring file.

**Exit code 3** if git diff fails (error includes valid base examples).

## heatmap incident create

Record a production incident for a file.

```sh
heatmap incident create --file <path> --severity <level> --description <text> [--date YYYY-MM-DD] [--json]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--file` | Yes | File path |
| `--severity` | Yes | `critical`, `high`, `medium`, or `low` |
| `--description` | Yes | What happened |
| `--date` | No | Incident date (default: today) |

Incidents are stored in `.heatmap/incidents.json` and feed into the heat score on next `heatmap analyze`.

## heatmap incident list

List recorded incidents.

```sh
heatmap incident list [--file <path>] [--json]
```

## heatmap report

Generate a heat distribution report.

```sh
heatmap report [--limit N] [--json]
```

Text output shows files grouped by tier with blast radius reasoning. JSON output returns the full heatmap data.

## heatmap dashboard

Generate an interactive HTML dashboard and open it in the browser.

```sh
heatmap dashboard [--output <path>] [--no-open]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `.heatmap/dashboard.html` | Output file path |
| `--no-open` | `false` | Don't open in browser |

The dashboard has two views: a treemap (visual overview) and a file explorer (hierarchical tree). Both support tier filtering, search, and a detail panel with blast radius reasoning.

## heatmap github install

Generate a GitHub Actions workflow for automated PR triage.

```sh
heatmap github install
```

Creates `.github/workflows/heatmap-triage.yml`.

## heatmap agent-context

Output machine-readable JSON describing all commands, flags, exit codes, and available models.

```sh
heatmap agent-context
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

| Variable | Required | Description |
|----------|----------|-------------|
| `OPENROUTER_API_KEY` | For LLM analysis | OpenRouter API key. Not needed with `--no-llm`. |
