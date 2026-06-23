# How It Works

## The Problem

Static analysis tools count branches, measure complexity, and trace imports. They can tell you that `auth.rs` has cyclomatic complexity 23. They cannot tell you that `auth.rs` handles OIDC token validation and that a bug in it means unauthorized access to every sandbox in the system.

Code Heatmap solves this by asking an LLM to read each file and answer: "if this code breaks, what's the blast radius?"

## Analysis Pipeline

```
highstakes analyze
  │
  ├── 1. Static Analysis
  │     ├── File discovery (respects .heatmap/config.yaml excludes)
  │     ├── Language detection (Go, Python, Rust, TypeScript, Java, ...)
  │     ├── Complexity (cyclomatic + cognitive via AST for Go, heuristic for others)
  │     ├── Dependency graph (import analysis, reverse dependency count)
  │     └── File size (lines, bytes)
  │
  ├── 2. Git Analysis
  │     ├── Commit frequency (total, last 90 days)
  │     ├── Unique contributors
  │     ├── Recent changes (last 10 commits per file)
  │     └── Bug-fix detection (commit message heuristics)
  │
  ├── 3. LLM Blast Radius Assessment
  │     ├── Filter: skip tests, config, docs, generated files
  │     ├── Cache check: skip files with unchanged content hash
  │     ├── Send file + path to LLM via OpenRouter
  │     ├── Parse structured JSON response
  │     └── Cache result by content SHA-256
  │
  └── 4. Scoring
        ├── Combine factors with weights (LLM 40%, static 30%, git 20%, manual 10%)
        ├── Assign tier (CRITICAL/HIGH/MEDIUM/LOW)
        └── Calculate review requirements per tier
```

## LLM Assessment

Each source file is sent to the LLM with a prompt asking it to score four impact dimensions on a 0-100 scale:

| Dimension | What It Measures | Example |
|-----------|-----------------|---------|
| Security | Auth, crypto, sandbox isolation, access control | "Handles OIDC token validation" |
| Data | PII, persistence, data integrity, schema | "Writes user records to database" |
| Availability | Service lifecycle, infrastructure, single points of failure | "Manages VM process lifecycle" |
| User | User-facing paths, API contracts, UX-critical flows | "Renders checkout page" |

The LLM also returns:
- `blast_radius_summary`: one sentence describing what breaks if the file has a bug
- `critical_reason`: why this file matters (empty for low-impact files)

### Scoring Guide in the Prompt

The prompt includes explicit calibration:

- **0-20**: Internal helper, formatting, logging. Cosmetic issues only.
- **21-40**: Utility code. Minor functionality loss.
- **41-60**: Business logic. Features malfunction.
- **61-80**: Core infrastructure. Service degradation or partial outage.
- **81-100**: Security boundary, auth, crypto, sandbox. Security breach, data loss, or complete outage.

## Heat Score Formula

The aggregate heat score (0-100) is a weighted combination:

### With LLM (default)

| Factor | Weight | Source |
|--------|--------|--------|
| LLM Blast Radius | 40% | Max of security/data/availability/user scores |
| Dependency Centrality | 15% | How many files import this one (normalized 0-1) |
| Complexity | 15% | Cyclomatic + cognitive complexity |
| Test Coverage Risk | 10% | 100 minus coverage percent |
| Incident History | 10% | Manual incident records (if any) |
| Change Frequency | 10% | Commits in last 90 days |

Weights normalize against available data. If git history is unavailable, its weight redistributes to other factors.

### Without LLM (`--no-llm`)

Path-based heuristics replace the LLM assessment. Files with "auth", "token", "payment" in their path score higher on user impact and data sensitivity. This produces weaker differentiation.

## Tier Assignment

| Tier | Score Range | Review Requirements |
|------|-------------|---------------------|
| CRITICAL | 86-100 | 2 senior reviewers, security scan, no auto-merge |
| HIGH | 61-85 | 2 reviewers, integration tests, no auto-merge |
| MEDIUM | 31-60 | 1 reviewer |
| LOW | 0-30 | Auto-merge safe |

## Caching

LLM assessments are cached in `.heatmap/cache/` by SHA-256 hash of file content. If a file hasn't changed since last analysis, its cached assessment is reused. This makes re-analysis near-free.

Cache files are JSON containing the four impact scores, summary, and critical reason. Delete `.heatmap/cache/` or run `highstakes analyze --force` to re-assess everything.

## File Filtering

Not all files are sent to the LLM. Skipped:

- Test files (`*_test.go`, `test_*.py`, `*.spec.ts`, etc.)
- Generated files (`*.pb.go`, `*.generated.*`)
- Config/docs (`.yaml`, `.toml`, `.json`, `.md`)
- Lock files (`go.sum`, `package-lock.json`)
- Excluded directories (configured in `.heatmap/config.yaml`)
