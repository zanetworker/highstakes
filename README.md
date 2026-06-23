# Code Heatmap

**Identify which code paths would hurt the most if broken.**

Uses LLM-assisted blast radius analysis to score every file in your repo by security, data, availability, and user impact. Surfaces reasoning so you know *why* a file is critical, not just that it is.

## Quick Start

```bash
# Install
go install github.com/zanetworker/code-heatmap/cmd/heatmap@latest

# Set OpenRouter API key (for LLM blast radius analysis)
export OPENROUTER_API_KEY="sk-or-..."

# Analyze a repo
cd /path/to/repo
heatmap init
heatmap analyze

# See results
heatmap list --tier high
heatmap get src/auth/oidc.rs
heatmap              # Interactive TUI
```

## What It Does

Reads every source file, sends it to an LLM asking "if this breaks, what's the blast radius?", and scores it across four dimensions:

- **Security** (auth boundaries, crypto, sandbox isolation)
- **Data** (PII handling, persistence, data integrity)
- **Availability** (service lifecycle, infrastructure)
- **User Impact** (user-facing paths, API contracts)

The LLM assessment is the primary signal (40% weight), combined with static analysis (complexity, dependency centrality) and git history (change frequency, incidents).

### Example Output

```bash
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

```bash
$ heatmap list --tier high --limit 5
🔥🔥  73  python/openshell/sandbox.py       Sandbox isolation breach risk
🔥🔥  72  python/openshell/_proto/...       Import failures in security-critical sandbox logic
🔥🔥  69  crates/.../runtime.rs             Host network misconfiguration, VM launch failures
🔥🔥  67  crates/.../disposition.rs         Corrupted security event disposition values
🔥🔥  65  crates/.../embedded_runtime.rs    Complete loss of VM functionality
```

## Tier System

| Tier | Score | What It Means | Review Required |
|------|-------|---------------|-----------------|
| 🔥🔥🔥 CRITICAL | 86-100 | Security breach, data loss, or full outage if broken | 2 senior reviewers + security scan |
| 🔥🔥 HIGH | 61-85 | Service degradation or partial outage | 2 reviewers + integration tests |
| 🔥 MEDIUM | 31-60 | Feature malfunction | 1 reviewer |
| 🟢 LOW | 0-30 | Cosmetic or minor functionality loss | Auto-review safe |

## Commands

### Core

```bash
heatmap init                    # Create .heatmap/ config
heatmap analyze                 # Scan repo + LLM blast radius assessment
heatmap                         # Interactive TUI explorer
```

### Query

```bash
heatmap get <file>              # Score + reasoning for one file
heatmap get <file> --json       # Machine-readable output
heatmap list                    # All files sorted by heat
heatmap list --tier high        # Filter by tier
heatmap list --limit 20         # Cap results
heatmap list --tier critical --json  # Structured output for agents
```

### PR Risk Assessment

```bash
heatmap pr check                # Score current diff vs main
heatmap pr check --base dev     # Score against different base
heatmap pr check --json         # Machine-readable for CI
```

### Incident Tracking

```bash
heatmap incident create \
  --file src/auth/jwt.rs \
  --severity high \
  --description "Token validation bypass" \
  --date 2026-06-15

heatmap incident list
heatmap incident list --file src/auth/jwt.rs
```

### Reports

```bash
heatmap report                  # Heat distribution with reasoning
heatmap report --limit 5        # Top 5 per tier
heatmap report --json           # Full heatmap as JSON
```

### GitHub Integration

```bash
heatmap github install          # Create GitHub Action workflow
```

Posts a risk assessment comment on every PR with file-level scores and review requirements.

### Agent Introspection

```bash
heatmap agent-context           # Machine-readable command schema
```

Returns versioned JSON describing all commands, flags, exit codes, and available models. Designed for AI agents to discover the CLI surface programmatically.

## LLM Models

Uses [OpenRouter](https://openrouter.ai) for model access. One API key, any model.

```bash
heatmap analyze                              # Default: DeepSeek V4 Flash (~$0.15/repo)
heatmap analyze --model deepseek/deepseek-v4-pro    # Better accuracy
heatmap analyze --model z-ai/glm-5.2               # Frontier open-weights
heatmap analyze --model openai/gpt-5.4-mini         # Safest JSON
heatmap analyze --model google/gemini-3-flash       # Fastest
heatmap analyze --model anthropic/claude-haiku-4.5   # Best reasoning
heatmap analyze --no-llm                            # Static only (no API key needed)
```

Assessments are cached by file content hash in `.heatmap/cache/`. Re-analysis only re-assesses changed files.

## Configuration

`.heatmap/config.yaml` (created by `heatmap init`):

```yaml
tiers:
  critical: 86
  high: 61
  medium: 31
  low: 0
```

## How Scoring Works

When LLM blast radius is available (default):

| Factor | Weight | Source |
|--------|--------|--------|
| **LLM Blast Radius** | **40%** | Max of security/data/availability/user scores |
| Dependency Centrality | 15% | Static import graph analysis |
| Complexity | 15% | Cyclomatic + cognitive complexity |
| Test Coverage Risk | 10% | Inverse of coverage % |
| Incident History | 10% | Manual incident records |
| Change Frequency | 10% | Git commit churn |

Without LLM (`--no-llm`), path-based heuristics replace the blast radius assessment with lower accuracy.

## Exit Codes

```
0  Success
1  Internal error
2  Invalid input (bad flags, missing required params)
3  External dependency failure (git, API)
4  Not found (file not in heatmap)
```

## Requirements

- Go 1.25+
- Git (for change frequency analysis)
- `OPENROUTER_API_KEY` environment variable (for LLM analysis; optional with `--no-llm`)

## Related

- [Agentic Code Review](https://addyo.substack.com/p/agentic-code-review) by Addy Osmani
- [Semantically-Seeded Graph-Propagated Impact Analysis](https://arxiv.org/abs/2606.18855) (Jun 2026)
- [BitsAI-CR: Two-Stage Code Review at ByteDance](https://arxiv.org/abs/2501.15134) (Jan 2025)
- [c-CRAB: Code Review Agent Benchmark](https://arxiv.org/abs/2603.23448) (Mar 2026)
- [GitNexus: MCP-native blast radius analysis](https://github.com/nicholasgriffintn/gitnexus)
- [OpenSSF Criticality Score](https://openssf.org/projects/criticality-score/)

## License

MIT
