# LLM-Assisted Blast Radius Analysis

## Problem

Static analysis alone cannot differentiate code by blast radius. A 400-file Rust repo analyzed with heuristics produces 382 LOW scores and zero actionable differentiation. Path matching ("auth" in filename) catches obvious cases but misses most critical code.

## Solution

Send each source file to an LLM with a structured prompt asking it to assess blast radius across four dimensions: security, data, availability, user impact. Store assessments in heatmap.json. Only re-assess files that changed since last analysis.

## Architecture

```
heatmap analyze
  |
  ├── Static analysis (existing) -> complexity, imports, size
  ├── Git analysis (existing) -> churn, authors, incidents  
  └── LLM analysis (new) -> blast radius assessment per file
        |
        ├── Read file content
        ├── Send to LLM API with structured prompt
        ├── Parse structured response (security, data, availability, user scores)
        ├── Cache assessment keyed by file content hash
        └── Feed scores into existing weight formula
```

## LLM Assessment Schema

Each file gets assessed on four dimensions (0-100):

```json
{
  "security_impact": 85,
  "data_impact": 60,
  "availability_impact": 40,
  "user_impact": 70,
  "blast_radius_summary": "Handles OIDC token validation. Breakage allows unauthorized access.",
  "critical_reason": "Authentication boundary"
}
```

## Prompt Design

The prompt asks the LLM to act as a security-aware code reviewer assessing blast radius. It provides the file content and asks for structured JSON output. The prompt emphasizes: "If this file has a bug, what breaks?"

Key instructions in the prompt:
- Score 0 = breakage has no external impact (internal helper, formatting)
- Score 100 = breakage causes security breach, data loss, or full outage
- Consider what the code DOES, not how complex it is
- A simple 10-line auth check can be more critical than a 500-line parser

## Scoring Integration

The LLM assessment replaces the current `user_impact` and `data_sensitivity` heuristics with real semantic understanding. New weight distribution:

| Factor | Weight | Source |
|--------|--------|--------|
| LLM blast radius (max of 4 dimensions) | 40% | LLM |
| Dependency centrality | 15% | Static |
| Complexity | 15% | Static |
| Change frequency | 10% | Git |
| Incident history | 10% | Manual |
| Test coverage risk | 10% | Static/placeholder |

The LLM blast radius is the primary signal. Static analysis provides secondary differentiation.

## Caching

- Cache key: SHA-256 of file content
- Cache location: `.heatmap/cache/` directory
- On re-analysis, skip files whose content hash matches cache
- `heatmap analyze --force` bypasses cache and re-assesses all files

This means initial analysis costs ~$1-2, subsequent runs cost near zero (only changed files).

## API Configuration

- Default provider: Anthropic (Claude haiku for cost efficiency)
- API key: `ANTHROPIC_API_KEY` environment variable
- Fallback: `--no-llm` flag falls back to static-only analysis (current behavior)
- Concurrency: 10 parallel requests by default, configurable with `--concurrency`

## CLI Changes

```bash
# Default: LLM-assisted analysis
heatmap analyze

# Skip LLM (static only, current behavior)
heatmap analyze --no-llm

# Force re-assess all files
heatmap analyze --force

# Use specific model
heatmap analyze --model claude-haiku-4-5-20251001
```

## File Filtering

Not all files need LLM assessment. Skip:
- Test files (*_test.go, *_test.rs, test_*.py)
- Generated files (*.pb.go, *.generated.*)
- Config files (*.yaml, *.toml, *.json)
- Documentation (*.md)
- Vendored/cached code

Only assess source files that could contain business logic.

## Error Handling

- API errors: Log warning, fall back to static-only score for that file
- Rate limiting: Exponential backoff with jitter
- Malformed LLM response: Retry once, then fall back to static
- Missing API key: Print clear error message suggesting --no-llm flag

## Success Criteria

Run `heatmap analyze` on OpenShell (382 files). The output should show:
- Auth files in CRITICAL tier
- Sandbox isolation files in CRITICAL/HIGH tier  
- TLS/crypto files in HIGH tier
- Policy enforcement in HIGH tier
- UI/formatting/logging files in LOW tier
- Clear differentiation across tiers (not all the same)
