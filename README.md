# Code Heatmap

**Know which code would hurt the most if it broke.**

Code Heatmap uses LLM-assisted blast radius analysis to score every file in your repository. It tells you *why* a file is critical ("sandbox isolation boundary, breakage causes container escapes") not just *that* it is. Point it at any codebase and immediately see where human review matters most.

- **LLM blast radius scoring** sends each file to an LLM asking "if this breaks, what's the impact?" and scores across security, data, availability, and user impact
- **Treemap dashboard** gives you a visual overview where your eye immediately goes to the biggest, reddest blocks
- **File explorer** shows directory hierarchy with heat scores, blast radius reasoning inline, and collapsible tree navigation
- **Agentic CLI** with `--json` on every command, `agent-context` for machine introspection, and structured exit codes
- **Multi-model** via [OpenRouter](https://openrouter.ai): DeepSeek V4 Flash (~$0.15/repo), GLM-5.2, GPT-5.4 Mini, Gemini 3 Flash, Claude Haiku
- **Cached** by content hash; re-analysis only re-assesses changed files

<p align="center">
  <img src="docs/images/treemap-view.png" width="800" alt="Treemap view showing files grouped by module, sized by lines of code, colored by heat score">
</p>

<p align="center">
  <img src="docs/images/explorer-view.png" width="800" alt="Explorer view showing collapsible file tree with heat scores and blast radius reasoning">
</p>

## Quick Start

**1.** Install:

```sh
go install github.com/zanetworker/code-heatmap/cmd/heatmap@latest
```

**2.** Set your OpenRouter API key:

```sh
export OPENROUTER_API_KEY="sk-or-..."
```

**3.** Analyze a repo:

```sh
cd /path/to/repo
heatmap init
heatmap analyze
```

**4.** See results:

```sh
heatmap dashboard    # Interactive HTML treemap + explorer
heatmap              # Terminal TUI
heatmap list --tier high
heatmap get src/auth/oidc.rs
```

## How It Works

1. **Static analysis** scans all source files for complexity, dependency centrality, and import graph
2. **Git analysis** measures change frequency, contributor count, and commit patterns
3. **LLM assessment** sends each source file to an LLM asking "if this code has a bug, what breaks?" and gets structured scores across four impact dimensions
4. **Heat scoring** combines LLM blast radius (40% weight) with static signals into a 0-100 score per file
5. **Tier assignment** maps scores to review requirements: CRITICAL (86+), HIGH (61-85), MEDIUM (31-60), LOW (0-30)

## Tier System

| Tier | Score | What Breaks | Review Required |
|------|-------|-------------|-----------------|
| 🔥🔥🔥 **CRITICAL** | 86-100 | Security breach, data loss, full outage | 2 senior reviewers + security scan |
| 🔥🔥 **HIGH** | 61-85 | Service degradation, partial outage | 2 reviewers + integration tests |
| 🔥 **MEDIUM** | 31-60 | Feature malfunction | 1 reviewer |
| 🟢 **LOW** | 0-30 | Cosmetic or minor loss | Auto-review safe |

## Commands

### Analyze

```sh
heatmap init                                        # Create .heatmap/ config
heatmap analyze                                     # Full analysis with LLM
heatmap analyze --model z-ai/glm-5.2                # Use a different model
heatmap analyze --no-llm                            # Static only (no API key needed)
```

### Visualize

```sh
heatmap dashboard                                   # HTML treemap + explorer (opens browser)
heatmap                                             # Terminal TUI
```

### Query

```sh
heatmap get <file>                                  # Score + blast radius reasoning
heatmap get <file> --json                           # Machine-readable
heatmap list                                        # All files sorted by heat
heatmap list --tier high --limit 10                 # Filter and cap
heatmap list --json                                 # Structured output for agents
heatmap report                                      # Heat distribution report
```

<details><summary><b>Example: heatmap get</b></summary>

```
$ heatmap get python/openshell/sandbox.py
python/openshell/sandbox.py  🔥🔥 HIGH  (score: 73)

Blast Radius (LLM-assessed):
  Sandbox management and execution failures could compromise isolation,
  leading to security breaches or service outage.
  Reason: Sandbox isolation is a security boundary.
  Security: 95  Data: 90  Availability: 90  User: 90

Risk Factors:
  Dependency Centrality:  8 (1 imports)
  Change Frequency:       0 (0 commits/90d)
  Complexity:             100 (cyclomatic: 53)

Review: 2 reviewers, ~45 min, auto-merge blocked
```

</details>

### PR Risk Assessment

```sh
heatmap pr check                                    # Score diff vs main
heatmap pr check --base dev                         # Different base branch
heatmap pr check --json                             # For CI pipelines
```

### Incident Tracking

```sh
heatmap incident create --file src/auth.rs \
  --severity high --description "Token bypass"      # Record incident
heatmap incident list                               # View all incidents
```

### GitHub Integration

```sh
heatmap github install                              # Create GitHub Action workflow
```

Posts a risk assessment comment on every PR with file scores and review requirements.

### Agent Introspection

```sh
heatmap agent-context                               # Full command schema as JSON
```

Returns versioned JSON with all commands, flags, exit codes, and available models. Designed for AI agents to discover the CLI surface programmatically.

## Models

Uses [OpenRouter](https://openrouter.ai) for model access. One API key, any model.

| Model | Cost / 500 files | Best For |
|-------|-----------------|----------|
| `deepseek/deepseek-v4-flash` (default) | ~$0.15 | Cheapest viable option |
| `deepseek/deepseek-v4-pro` | ~$0.50 | Best accuracy per dollar |
| `z-ai/glm-5.2` | ~$3-5 | Frontier open-weights |
| `openai/gpt-5.4-mini` | ~$0.90 | Safest JSON reliability |
| `google/gemini-3-flash` | ~$0.50 | Fastest |

```sh
heatmap analyze --model deepseek/deepseek-v4-pro
```

## Scoring

| Factor | Weight | Source |
|--------|--------|--------|
| **LLM Blast Radius** | **40%** | Max of security/data/availability/user impact |
| Dependency Centrality | 15% | Static import graph |
| Complexity | 15% | Cyclomatic + cognitive |
| Test Coverage Risk | 10% | Inverse of coverage |
| Incident History | 10% | Manual records |
| Change Frequency | 10% | Git churn |

Without LLM (`--no-llm`), path heuristics replace blast radius with lower accuracy.

## Configuration

`.heatmap/config.yaml`:

```yaml
tiers:
  critical: 86
  high: 61
  medium: 31
  low: 0
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Internal error |
| 2 | Invalid input |
| 3 | External dependency failure |
| 4 | Not found |

## Requirements

- Go 1.25+
- Git
- `OPENROUTER_API_KEY` (optional with `--no-llm`)

## Related

- [Agentic Code Review](https://addyo.substack.com/p/agentic-code-review) by Addy Osmani
- [Semantically-Seeded Graph-Propagated Impact Analysis](https://arxiv.org/abs/2606.18855) (Jun 2026)
- [BitsAI-CR: Two-Stage Code Review at ByteDance](https://arxiv.org/abs/2501.15134) (Jan 2025)
- [c-CRAB: Code Review Agent Benchmark](https://arxiv.org/abs/2603.23448) (Mar 2026)
- [OpenSSF Criticality Score](https://openssf.org/projects/criticality-score/)

## License

MIT
