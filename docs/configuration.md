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

These thresholds apply to the **heat score**, which sets the tier of files without an LLM assessment (`--no-llm`, no API key, or files the LLM skips). Files with an LLM assessment get their tier from the impact/likelihood quadrant instead: impactful means impact at or above `max(high, the repo's top-20% impact)`, and likely means the top 20% of the repo by likelihood (see [How It Works](how-it-works.md#the-quadrant)). So `high` is also the floor for "impactful"; `critical` and `medium` only affect unassessed files.

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

Matching is case-insensitive. Without an alias, GitHub noreply addresses (`123+alice@users.noreply.github.com`) become `@alice`; other authors appear by email in local outputs (`get`, `create codeowners`, the dashboard), which CODEOWNERS also accepts. PR comments (`pr check --format markdown`) never show an email: a review request without a known handle asks for "someone who knows" the file, and any other unmapped author reads "a top owner", so add an alias to name them. Use a `.mailmap` file to merge an author's multiple emails first; it is applied automatically. Authors whose name or email contains `[bot]` are ignored.

## Tour Supporting Components

`get tour` hides supporting components (CI, deployment, scripts, examples) unless `--include-supporting` is given. The same rule marks runtime units supporting when their entry point sits in such a component (see `list units`). A component is supporting when any of its path segments is `examples`, `scripts`, `.github`, `docs`, `deploy`, `tests`, `e2e`, `benchmarks`, `bench`, `fixtures`, `testdata`, `vendor` or `third_party`. Add more with globs (Go `path.Match` syntax, matched against the component and each of its parent directories); they extend the defaults:

```yaml
tour:
  supporting:
    - tools            # tools/ and every component under it
    - crates/*-demo    # demo crates
```

A malformed glob makes `get tour` exit with code 2.

## Agent Autonomy

Cap how far AI agents may act on any file (levels in the [CLI reference](cli-reference.md#agent-autonomy): 3 auto-merge, 2 auto-fix with a human merging, 1 propose only, 0 assist only):

```yaml
autonomy:
  max_level: 2    # 0-3, default 3
```

The cap applies on top of the per-file level, which is already conservative (files without an LLM-assessed impact stay at 1 or below). `analyze` records the capped level per file; `create policy --agents`, `list tasks` and `pr check` read the cap on every run. A value outside 0-3 makes `analyze`, `create policy` and `list tasks` exit 2 with the valid values (0, 1, 2, 3); a config file that does not parse makes them exit 2 with the YAML error. None falls back to the default cap of 3. `check calibration` reads only the tier thresholds and uses the default tiers when the config cannot be read. `pr check` alone keeps running: it warns on stderr and caps every file at 1, so CI never grants more than intended.

## Architecture Declarations

`.heatmap/architecture.yaml` is optional. It declares runtime links and placements that imports and manifests cannot show, so generated pictures (`get architecture`, the dashboard unit view, `list units`) draw the real topology. Unit names are the ones `list units --include-supporting` prints.

```yaml
links:
  - from: worker
    to: api
    label: gRPC job stream               # arrow label (SeeCode shows up to 28 characters)
    both: true                           # two-way arrow
    evidence: docs/architecture.md:42
  - from: api
    to: scheduler
    label: enqueues jobs
placement:
  - unit: worker
    runs_in: worker pod                  # group label (up to 21 characters fit beside "(declared)")
    evidence: deploy/helm/app/templates/worker.yaml:12
```

| Field | Required | Meaning |
|-------|----------|---------|
| `links[].from`, `links[].to` | yes | Unit names; must differ |
| `links[].label` | no | What travels; defaults to "runtime link" |
| `links[].both` | no | The link goes both ways |
| `links[].evidence` | no | `file:line` backing the claim; shown in `list units` and the dashboard |
| `placement[].unit` | yes | Unit name; one placement per unit |
| `placement[].runs_in` | yes | Where it runs ("workload pod", "gateway deployment"); overrides inferred placement in diagrams |
| `placement[].evidence` | no | `file:line` backing the claim |

Declared links draw dashed, and declared placements carry "declared", everywhere they appear; inferred ones stay solid and "inferred". The file is read on every command, so editing it needs no new `analyze`. Unknown fields, a missing required field, a self link, a second placement for one unit, or a unit name not in the heatmap make `get architecture`, `list units` and `dashboard` exit with code 2, listing every problem and the valid unit names. Write evidence you checked: the picture is only as true as the declarations.

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

Chosen on the [calibration benchmark](../bench/README.md) (7 repos, about 5,500 files assessed per model, October 2026): the four models rank files by impact in largely the same order (rank correlation 0.72 to 0.90), so a cheap model is enough. Costs are upper estimates from token counts.

## Caching

LLM assessments are cached in `.heatmap/cache/` by a SHA-256 hash of the model ID, the prompt version and the file content. On re-analysis, only files whose content changed since last run are re-assessed, which makes subsequent runs near-free. Switching `--model`, or a HighStakes release that changes the assessment prompt (and bumps its version), invalidates the cache, so those runs re-assess every file.

Force a full re-assessment with:

```sh
highstakes analyze --force
```

## Static-Only Mode

Skip LLM analysis entirely (no API key needed):

```sh
highstakes analyze --no-llm
```

This skips the LLM blast radius only. Scores still use complexity, dependency centrality, path keywords, git history (churn, authors) and recorded incidents; impact is an estimate and tiers come from the heat score thresholds. Less accurate but free and fast.

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
