# Getting Started

## Prerequisites

- Go 1.25+
- Git (for change frequency analysis)
- An [OpenRouter](https://openrouter.ai) API key (for LLM blast radius analysis)

## Install

```sh
go install github.com/zanetworker/highstakes/cmd/highstakes@latest
```

Or build from source:

```sh
git clone https://github.com/zanetworker/highstakes
cd highstakes
go install ./cmd/highstakes
```

The binary bundles parsers for every supported language (about 27 MB). Add `-ldflags="-s -w"` to either command to strip debug symbols and get about 19 MB.

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

On a 300-file repo this takes about 1-2 minutes and costs roughly $0.12 with the default model (GPT-6 Luna).

## View Results

Three ways to see the heatmap:

```sh
# Interactive HTML dashboard (opens browser)
highstakes dashboard

# Terminal TUI
highstakes

# CLI queries
highstakes list --tier high
highstakes get src/auth/oidc.rs
highstakes report
```

## What to Do With the Results

Every file gets a posture from two questions, "Will it break?" and "How bad if it breaks?", each shown with its repo cutoff. `highstakes get <file>` lists what to do before the next merge and what to put on the backlog; `highstakes dashboard` opens on the same actions as an inbox.

**Harden now** (tier critical): senior owner sign-off, regression and contract tests, auto-merge off on every PR that touches them. Gate them in CI with `pr check --fail-on critical` (see [CI Integration](ci-integration.md)).

**Protect** (tier high): owner review and no auto-merge; add tests first when none sit next to the file.

**Stabilize** (tier medium): regression tests when fixes recur, refactor what you touch. Never blocks.

**Routine** (tier low): normal review; auto-merge allowed by policy. Don't spend senior engineer time here.

## Next Steps

- [Configuration](configuration.md) to tune tier thresholds and exclude directories
- [CI Integration](ci-integration.md) to gate PRs automatically
- [CLI Reference](cli-reference.md) for all commands and flags
