# Configuration

All configuration lives in `.heatmap/config.yaml`, created by `highstakes init`.

## Tier Thresholds

Control which score ranges map to which tiers:

```yaml
tiers:
  critical: 86    # score >= 86
  high: 61        # score >= 61
  medium: 31      # score >= 31
  low: 0          # score >= 0
```

Lower the thresholds to be more aggressive (more files flagged as high risk). Raise them to be more conservative.

## Excluded Directories

Directories and patterns to skip during analysis. Prevents scanning vendored dependencies, virtual environments, and generated code.

```yaml
exclude:
  dirs:
    - node_modules
    - vendor
    - __pycache__
    - .venv
    - venv
    - site-packages
    - target
    - build
    - dist
    - .tox
    - .mypy_cache
    - .pytest_cache
    - .eggs
    - third_party
    - deps
  patterns:
    - "distribution-*"
    - "env*"
    - "*.egg-info"
```

Add project-specific exclusions here. The defaults cover Python, Go, Rust, JavaScript, and Java ecosystems.

## Owner Aliases

`pr check` reviewers and `create codeowners` owners come from git history, which only knows emails. Map emails to GitHub handles so the output can be used in CODEOWNERS and PR comments:

```yaml
owners:
  alice@example.com: "@alice"
  bob@corp.example: bob-gh      # the @ is added if missing
```

Matching is case-insensitive. Without an alias, GitHub noreply addresses (`123+alice@users.noreply.github.com`) become `@alice` and other authors appear by email, which CODEOWNERS also accepts. Use a `.mailmap` file to merge an author's multiple emails first; it is applied automatically. Authors whose name or email contains `[bot]` are ignored.

## Tour Supporting Components

`get tour` hides supporting components (CI, deployment, scripts, examples) unless `--include-supporting` is given. A component is supporting when any of its path segments is `examples`, `scripts`, `.github`, `docs`, `deploy`, `tests`, `e2e`, `benchmarks`, `fixtures`, `vendor` or `third_party`. Add more with globs (Go `path.Match` syntax, matched against the component and each of its parent directories); they extend the defaults:

```yaml
tour:
  supporting:
    - tools            # tools/ and every component under it
    - crates/*-demo    # demo crates
```

A malformed glob makes `get tour` exit with code 2.

## LLM Provider

HighStakes works with any OpenAI-compatible API. Three ways to configure it:

### Option 1: OpenRouter (default, easiest)

One API key, access to every model. Get a key at [openrouter.ai](https://openrouter.ai).

```sh
export OPENROUTER_API_KEY="sk-or-..."
highstakes analyze
```

### Option 2: Direct provider API

Point at any OpenAI-compatible endpoint (DeepSeek, OpenAI, vLLM, Ollama, etc.):

```sh
export HIGHSTAKES_API_KEY="your-key"
export HIGHSTAKES_API_URL="https://api.deepseek.com/v1/chat/completions"
highstakes analyze --model deepseek-chat
```

```sh
# OpenAI directly
export HIGHSTAKES_API_KEY="sk-..."
export HIGHSTAKES_API_URL="https://api.openai.com/v1/chat/completions"
highstakes analyze --model gpt-4.1-mini

# Local Ollama
export HIGHSTAKES_API_KEY="ollama"
export HIGHSTAKES_API_URL="http://localhost:11434/v1/chat/completions"
highstakes analyze --model llama3
```

### Option 3: No LLM

Static analysis only. No API key needed. Less accurate but free.

```sh
highstakes analyze --no-llm
```

## Model Selection

Override the model per-run:

```sh
highstakes analyze --model z-ai/glm-5.3-flash
```

Available models:

| Model | Cost / 500 files | Notes |
|-------|-----------------|----------|
| `openai/gpt-6-luna` | ~$0.19 | Default. Best impact ranking on the benchmark |
| `z-ai/glm-5.3-flash` | ~$0.04 | Cheapest; 2-3x slower |
| `deepseek/deepseek-v4.1-flash` | ~$0.13 | Open weights, can be self-hosted |
| `anthropic/claude-haiku-5.5` | ~$0.19 | Zero failed calls on the benchmark |

Chosen on the [calibration benchmark](../bench/README.md) (7 repos, 6,240 files, October 2026): the four models rank files by impact in largely the same order (rank correlation 0.72 to 0.90), so a cheap model is enough. Costs are estimates from token counts.

## Caching

LLM assessments are cached in `.heatmap/cache/` by file content hash (SHA-256). On re-analysis, only files whose content changed since last run are re-assessed. This makes subsequent runs near-free.

Force a full re-assessment with:

```sh
highstakes analyze --force
```

## Static-Only Mode

Skip LLM analysis entirely (no API key needed):

```sh
highstakes analyze --no-llm
```

This uses only static signals (complexity, dependency centrality, path heuristics). Produces less accurate results but is free and fast.

## Concurrency

Control how many files are assessed in parallel:

```sh
highstakes analyze --concurrency 20
```

Default is 10. Increase for faster analysis on large repos if your API rate limit allows it.

## .gitignore

Add `.heatmap/` to your `.gitignore` if you don't want to commit the heatmap data:

```
.heatmap/
```

Or commit it to share cached assessments across the team (recommended for CI).
