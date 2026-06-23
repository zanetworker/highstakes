# Getting Started

## Prerequisites

- Go 1.25+
- Git (for change frequency analysis)
- An [OpenRouter](https://openrouter.ai) API key (for LLM blast radius analysis)

## Install

```sh
go install github.com/zanetworker/highstakes/cmd/heatmap@latest
```

Or build from source:

```sh
git clone https://github.com/zanetworker/highstakes
cd highstakes
go install ./cmd/heatmap
```

## Set Up

Export your OpenRouter API key:

```sh
export OPENROUTER_API_KEY="sk-or-..."
```

Add it to your shell profile (`~/.zshrc`, `~/.bashrc`) to persist across sessions.

## First Analysis

```sh
cd /path/to/your/repo
highstakes init
highstakes analyze
```

`highstakes init` creates a `.heatmap/` directory with a `config.yaml` file. `highstakes analyze` scans the repo, sends source files to the LLM for blast radius assessment, and writes results to `.heatmap/heatmap.json`.

On a 300-file repo this takes about 1-2 minutes and costs roughly $0.15 with the default model (DeepSeek V4 Flash).

## View Results

Three ways to see the heatmap:

```sh
# Interactive HTML dashboard (opens browser)
highstakes dashboard

# Terminal TUI
heatmap

# CLI queries
highstakes list --tier high
highstakes get src/auth/oidc.rs
highstakes report
```

## What to Do With the Results

**HIGH/CRITICAL files**: These need careful human review on every PR that touches them. Set up branch protection or CI gates (see [CI Integration](ci-integration.md)).

**MEDIUM files**: Worth a human glance. One reviewer is sufficient.

**LOW files**: Safe for AI-assisted review or auto-merge. Don't spend senior engineer time here.

## Next Steps

- [Configuration](configuration.md) to tune tier thresholds and exclude directories
- [CI Integration](ci-integration.md) to gate PRs automatically
- [CLI Reference](cli-reference.md) for all commands and flags
