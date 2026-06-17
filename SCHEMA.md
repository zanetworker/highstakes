# Code Heatmap Schema

This document explains the heatmap data structure and how it's used.

## Overview

The heatmap is stored in `.heatmap/heatmap.json` as a JSON file. It contains:

1. **Metadata** — Repo info, analysis timestamp, commit SHA
2. **Files** — Per-file heat scores and risk factors
3. **Incidents** — Historical production incidents
4. **Annotations** — Manual tags and overrides

## File Structure

```
.heatmap/
├── heatmap.json       # Main heatmap data
├── incidents.json     # Incident history (merged into heatmap.json)
└── config.yaml        # Analysis configuration
```

## Heat Score Calculation

Heat score is 0-100, calculated as weighted sum of factors:

| Factor | Weight | Description |
|--------|--------|-------------|
| Dependency Centrality | 20% | How many files import this? |
| Incident History | 25% | How many bugs has this caused? |
| Change Frequency | 10% | How often does this change? |
| User Impact | 20% | Is this user-facing/critical? |
| Data Sensitivity | 15% | Does this handle PII/secrets? |
| Test Coverage | -10% | Higher coverage = lower risk |
| Complexity | 10% | Cyclomatic/cognitive complexity |

**Formula:**
```
heat_score = (
  dependency_centrality.score * 20 +
  incident_history.score * 25 +
  change_frequency.score * 10 +
  user_impact.score * 20 +
  data_sensitivity.score * 15 +
  complexity.score * 10
) - (test_coverage.score * 10)

Clamped to [0, 100]
```

## Tier Thresholds

| Tier | Score Range | Review Requirements |
|------|-------------|---------------------|
| 🔥🔥🔥 **CRITICAL** | 86-100 | 2 senior humans + security scan + no auto-merge |
| 🔥🔥 **HIGH** | 61-85 | 2 humans + heterogeneous AI review |
| 🔥 **MEDIUM** | 31-60 | 1 human + AI reviewer + tests required |
| 🟢 **LOW** | 0-30 | Linter + single AI reviewer + auto-merge eligible |

## Factor Scores (0-100)

Each factor produces a 0-100 score. Higher = more risky.

### Dependency Centrality (0-1 normalized to 0-100)

```
centrality = imported_by_count / max_imported_by_in_repo
score = centrality * 100
```

Files with many importers score higher.

### Incident History

```
if incident_count == 0:
  score = 0
else:
  base = min(incident_count * 20, 80)  # Cap at 80
  recency_boost = 20 if last_incident < 90 days ago else 0
  severity_boost = (critical * 10 + high * 5 + medium * 2)
  score = min(base + recency_boost + severity_boost, 100)
```

### Change Frequency

```
if commits_last_90d == 0:
  score = 0  # Stable
else:
  churn = commits_last_90d / 90  # Commits per day
  score = min(churn * 50, 100)
```

Frequently changing files score higher.

### User Impact

```
score = 0
if user_facing: score += 40
if affects_auth: score += 30
if affects_data: score += 20
if affects_payments: score += 30
score = min(score, 100)
```

### Data Sensitivity

```
score = 0
if handles_pii: score += 50
if handles_secrets: score += 40
if handles_financial: score += 50
score = min(score, 100)
```

### Test Coverage

```
score = 100 - coverage_percent
if has_integration_tests: score -= 20
score = max(score, 0)
```

Higher coverage = lower score (this is subtracted from heat).

### Complexity

```
normalized_cyclomatic = min(cyclomatic / 20, 1.0)  # Cap at 20
normalized_cognitive = min(cognitive / 30, 1.0)    # Cap at 30
score = (normalized_cyclomatic * 50) + (normalized_cognitive * 50)
```

## Review Requirements

Based on tier:

```go
switch tier {
case "critical":
  min_reviewers = 2
  requires_senior = true
  requires_security_scan = true
  auto_merge = false
  estimated_review_time_minutes = 60

case "high":
  min_reviewers = 2
  requires_senior = false
  requires_security_scan = false
  auto_merge = false
  estimated_review_time_minutes = 45

case "medium":
  min_reviewers = 1
  requires_senior = false
  requires_security_scan = false
  auto_merge = false
  estimated_review_time_minutes = 20

case "low":
  min_reviewers = 0
  requires_senior = false
  requires_security_scan = false
  auto_merge = true
  estimated_review_time_minutes = 5
}
```

## PR Risk Score

Aggregate PR risk = max heat score of changed files.

```
pr_heat = max(file.heat_score for file in changed_files)
pr_tier = tier_for_score(pr_heat)
```

PR inherits review requirements from its highest-tier file.

## Circuit Breaker Signals

These are NOT in the schema but computed on-demand during PR scoring:

- ✓ Large diff (>500 lines)
- ✓ Many file types (>3 languages)
- ✓ Low test ratio (test lines / code lines < 0.3)
- ✓ No statement of purpose (check PR description)

## Manual Annotations

Users can override scores:

```json
{
  "annotations": {
    "src/payment/processor.ts": {
      "override_tier": "critical",
      "tags": ["payment-flow", "pci-scope"],
      "owner": "alice@example.com",
      "notes": "This handles credit card tokens. Always require security review."
    }
  }
}
```

`override_tier` takes precedence over calculated tier.

## Incident Schema

```json
{
  "id": "INC-2026-001",
  "file": "src/auth/jwt.ts",
  "date": "2026-05-12",
  "severity": "high",
  "description": "JWT expiry edge case caused login failures",
  "caused_by_commit": "a7981ec",
  "fixed_by_commit": "3df7661",
  "downtime_minutes": 45,
  "users_affected": 1200
}
```

Incidents feed into `incident_history` factor.

## Example Output

```json
{
  "version": "1.0.0",
  "metadata": {
    "repo_path": "/Users/adel/code/myapp",
    "analyzed_at": "2026-06-17T10:30:00Z",
    "commit_sha": "a7981ec",
    "branch": "main",
    "total_files": 456,
    "languages": {
      "Go": 234,
      "TypeScript": 189,
      "Python": 33
    }
  },
  "files": {
    "src/auth/jwt.ts": {
      "path": "src/auth/jwt.ts",
      "heat_score": 95,
      "tier": "critical",
      "language": "TypeScript",
      "size": {
        "lines": 342,
        "bytes": 12456
      },
      "factors": {
        "dependency_centrality": {
          "score": 0.92,
          "import_count": 18,
          "exported_symbols": 5
        },
        "incident_history": {
          "score": 85,
          "incident_count": 3,
          "last_incident": "2026-05-12",
          "severity_breakdown": {
            "critical": 0,
            "high": 2,
            "medium": 1,
            "low": 0
          }
        },
        "change_frequency": {
          "score": 45,
          "commit_count": 67,
          "commits_last_90d": 8,
          "unique_authors": 4
        },
        "user_impact": {
          "score": 95,
          "user_facing": true,
          "affects_auth": true,
          "affects_data": false,
          "affects_payments": false
        },
        "data_sensitivity": {
          "score": 100,
          "handles_pii": true,
          "handles_secrets": true,
          "handles_financial": false
        },
        "test_coverage": {
          "score": 15,
          "coverage_percent": 85,
          "test_count": 12,
          "has_integration_tests": true
        },
        "complexity": {
          "score": 42,
          "cyclomatic": 15,
          "cognitive": 18,
          "function_count": 8
        }
      },
      "dependencies": {
        "imported_by": [
          {"path": "src/api/middleware.ts", "heat_score": 72},
          {"path": "src/api/auth-handler.ts", "heat_score": 68}
        ],
        "imports": [
          {"path": "src/utils/crypto.ts", "heat_score": 55}
        ]
      },
      "recent_changes": [
        {
          "date": "2026-06-10",
          "message": "Fix token expiry edge case",
          "author": "alice@example.com",
          "sha": "3df7661",
          "pr_number": 1245,
          "had_incident": false
        }
      ],
      "review_requirements": {
        "min_reviewers": 2,
        "requires_senior": true,
        "requires_security_scan": true,
        "requires_integration_tests": true,
        "auto_merge": false,
        "estimated_review_time_minutes": 60
      }
    }
  },
  "incidents": [
    {
      "id": "INC-2026-001",
      "file": "src/auth/jwt.ts",
      "date": "2026-05-12",
      "severity": "high",
      "description": "JWT expiry edge case",
      "caused_by_commit": "a7981ec",
      "fixed_by_commit": "3df7661",
      "downtime_minutes": 45,
      "users_affected": 1200
    }
  ],
  "annotations": {}
}
```

## Usage in Code

```go
// Load heatmap
heatmap, err := LoadHeatmap(".heatmap/heatmap.json")

// Query file score
file := heatmap.Files["src/auth/jwt.ts"]
fmt.Println(file.HeatScore)  // 95
fmt.Println(file.Tier)        // "critical"

// Check review requirements
if !file.ReviewRequirements.AutoMerge {
  fmt.Println("Manual review required")
}

// Calculate PR risk
prRisk := CalculatePRRisk(changedFiles, heatmap)
fmt.Println(prRisk.Tier)  // "high"
```

## Schema Version

Current version: `1.0.0`

Breaking changes increment major version. Tools should check `version` field and error on unsupported versions.
