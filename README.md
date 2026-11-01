<p align="center">
  <br>
  <img src="docs/images/treemap-view.png" width="820" alt="HighStakes treemap view">
  <br>
  <em>AI writes code faster than you can review it. See where you're needed and where AI can handle the rest.</em>
  <br>
  <br>
</p>

<h1 align="center">HighStakes</h1>

<p align="center">
  <strong>Know which code would hurt the most if it broke.</strong>
  <br>
  LLM-assisted blast radius analysis for every file in your repo.
  <br>
  <br>
  <a href="#quick-start">Quick Start</a> ·
  <a href="#how-it-works">How It Works</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#models">Models</a>
</p>

Point it at any codebase. In under 2 minutes, you know:

- **Where to focus**: "sandbox.py handles isolation. Breakage causes container escapes." (score: 73, HIGH)
- **What's safe**: "theme.rs sets terminal colors. Breakage causes cosmetic issues." (score: 6, LOW)
- **What to do**: 2 senior reviewers + security scan, or auto-merge OK

Static analysis alone can't do this. A complexity counter sees `auth.rs` and `theme.rs` as "both Rust files." The LLM reads the code and understands that one is a security boundary and the other formats colors.

Every file gets two scores, because they answer different questions:

- **Impact**: what breaks if this file has a bug (LLM blast radius).
- **Likelihood**: how likely this file is to need fixes (churn, complexity, centrality, incidents).

Together they place each file in a 2x2 that sets its tier and review guidance:

|  | Low likelihood | High likelihood (top 20% of the repo) |
|---|---|---|
| **High impact (>= 61)** | **Protect** (HIGH): stable but costly if broken; keep owners on review, never auto-merge | **Fragile and critical** (CRITICAL): senior review, add tests first |
| **Low impact** | **Low stakes** (LOW): light review | **Watch** (MEDIUM): changes and breaks often, limited damage; refactor or add tests |

## Quick Start

**1.** Install and set up:

```sh
go install github.com/zanetworker/highstakes/cmd/highstakes@latest
export OPENROUTER_API_KEY="sk-or-..."
```

**2.** Analyze:

```sh
cd /path/to/repo
highstakes init && highstakes analyze
```

```
Heatmap generated: .heatmap/heatmap.json (305 files)

Heat Distribution:
  🔥🔥🔥 CRITICAL: 0 files
  🔥🔥  HIGH:     30 files
  🔥   MEDIUM:   141 files
  🟢   LOW:      134 files
```

**3.** See results:

```sh
highstakes dashboard     # Treemap, file explorer and architecture view (opens browser)
highstakes               # Terminal TUI
highstakes list --tier high
```

## Terminal TUI

Navigate the file tree with colored tier indicators. Select any file to see blast radius, impact dimensions, and review requirements.

<p align="center">
  <img src="docs/images/tui-view.png" width="820" alt="Terminal TUI with file tree, blast radius detail, and impact dimension bars">
</p>

## Dashboard

Four views, one detail panel. Click any file to see its impact and likelihood, quadrant guidance, blast radius reasoning, impact dimensions, and review requirements.

**Treemap**: blocks sized by code volume, colored by heat. Your eye goes to the big red blocks first.

**Explorer**: collapsible file tree sorted hottest-first, with heat bars and reasoning inline.

<p align="center">
  <img src="docs/images/explorer-view.png" width="820" alt="Explorer view with file tree, heat scores, and blast radius reasoning">
</p>

All views respond to tier filters (Critical / High / Medium / Low) and search; the treemap also has the size toggle (Lines vs Heat).

**Architecture**: one node per package (directory), laid out in dependency layers: entry points on top, foundations at the bottom. Node size is total lines, color is the highest impact in the package (what breaks if it breaks), and a dashed outline with an "N likely" badge marks packages holding files likely to need fixes. Each node names its hottest file. Arrows run from importer to dependency and carry the number of importing files. Click a package to list its files hottest-first in the detail panel, then click a file for its blast radius. Packages outside the current tier filter or search are dimmed.

**Quadrant**: every file as a dot, likelihood on x and impact on y, with the two thresholds drawn and each quadrant labelled with its review guidance. Click a dot to open the file in the detail panel.

## How It Works

1. **Static analysis** scans every source file for complexity, dependency centrality, import graph
2. **Git analysis** measures change frequency, contributor count, commit patterns
3. **LLM assessment** sends each file to an LLM: *"if this code has a bug, what breaks?"*
4. **Scoring** keeps two axes: likelihood from history and structure, impact from the LLM
5. **Tiering** places each file in the likelihood x impact quadrant and maps it to review requirements

The LLM returns structured scores across four dimensions:

| Dimension | What It Measures |
|-----------|-----------------|
| **Security** | Auth boundaries, crypto, sandbox isolation, access control |
| **Data** | PII handling, persistence, data integrity, schema |
| **Availability** | Service lifecycle, infrastructure, single points of failure |
| **User** | User-facing paths, API contracts, UX-critical flows |

### Why Two Axes

On the [calibration benchmark](bench/README.md) (seven repos, five languages), churn and complexity predicted which files needed fixes later: the ten top-ranked files on the primary repos were all fixed after the holdout cutoff. Blast radius was consistent across four LLMs but measures something else: a stable auth module can be critical and never break. Blending it into one score at 40% made fix prediction worse for every model. So HighStakes reports both and lets the quadrant decide.

| Likelihood factor | Weight | Source |
|--------|--------|--------|
| Change Frequency | 40 | Commits in last 90 days |
| Complexity | 30 | Cyclomatic + cognitive |
| Incident History | 25 | Manual records (when present) |
| Dependency Centrality | 10 | Import graph analysis |
| Path Keywords | 5 | "auth", "payment" and similar in the path |

Impact is the highest of the four LLM dimensions. Without an LLM it is an estimate from centrality and path keywords, shown with `~`, and tiers fall back to the heat score. `heat_score` stays in the JSON as `max(likelihood, impact)`. Details: [How It Works](docs/how-it-works.md#two-axes-likelihood-and-impact).

### Tier System

| Tier | Quadrant (with LLM) | Heat score (without LLM) | Review Required |
|------|------|-------|-----------------|
| 🔥🔥🔥 **CRITICAL** | Fragile and critical | 86-100 | 2 senior reviewers + security scan |
| 🔥🔥 **HIGH** | Protect | 61-85 | 2 reviewers + integration tests |
| 🔥 **MEDIUM** | Watch | 31-60 | 1 reviewer |
| 🟢 **LOW** | Low stakes | 0-30 | Auto-review safe |

## Commands

### Analyze

```sh
highstakes init                                         # Create .heatmap/ config
highstakes analyze                                      # Full LLM analysis (~$0.15)
highstakes analyze --model z-ai/glm-5.2                 # Different model
highstakes analyze --no-llm                             # Static only, no API key
```

### Visualize

```sh
highstakes dashboard                                    # HTML dashboard (browser)
heatmap                                              # Terminal TUI
```

### Query

```sh
highstakes get <file>                                   # Why impact, why likely, what to do
highstakes list --tier high --limit 10                  # Filter files
highstakes list --sort impact --limit 10                # Most impactful first (also: likelihood, heat)
highstakes report                                       # Distribution report
```

All commands support `--json` for machine-readable output.

<details open><summary><b>Example: highstakes get</b></summary>

`get` says why a file is impactful, why it is likely to break and what to do, quoting the file's numbers and where they rank in the repo:

```
$ highstakes get crates/openshell-supervisor-network/src/proxy.rs        # NVIDIA/OpenShell

crates/openshell-supervisor-network/src/proxy.rs  🔥🔥🔥 CRITICAL  (score: 92)

FRAGILE AND CRITICAL (impact 92, likelihood 50; likely starts at 33 in this repo)
Why impact is high: security 92 · availability 85 · user 85 · data 70
  "Core sandbox egress proxy; bugs can block traffic or bypass network policy."
  "Enforces sandbox network access and identity-bound policy"
Why it's likely to break:
  55 commits in the last 90 days, 28 authors overall (top 1% of this repo)
  479 functions, cyclomatic 1092 in one file (top 1%)
  11 files import it (top 5%)
  4 recent fixes; latest: "fix(network): align destination checks for direct connections (#5004)" (2026-10-28)
What to do:
  • Every PR: senior review and sign-off from a top owner (@johntmyers, rrhubenov@gmail.com)
  • Add to CODEOWNERS; never auto-merge
  • Split it: 479 functions in one file
  • Add regression tests around the 4 recent fixes
  • Keep its public interface stable: 11 importers; add contract tests
  • Spread knowledge: 28 authors change it; document its invariants and keep an owner on review

Review: 2 reviewers (senior), security scan, ~60 min, auto-merge blocked
```

Reasons and actions are deterministic (no extra LLM call); see [How It Works](docs/how-it-works.md#reasons-and-actions) for the thresholds. `pr check` lists the top three actions under each HIGH or CRITICAL file it touches, and the dashboard and TUI detail panels show the same three sections.

</details>

<details><summary><b>Example: highstakes list --sort impact</b></summary>

```
$ highstakes list --sort impact --limit 5        # hono

🔥🔥🔥  92  L  35  I  92  src/middleware/jwk/jwk.ts           JWT authentication failures can block protected routes ...
🔥🔥🔥  92  L  30  I  92  src/utils/jwt/jws.ts                JWS signing and verification failures disrupt JWT issuance ...
🔥🔥🔥  92  L  37  I  92  src/utils/jwt/jwt.ts                JWT signing and verification failures can disrupt authentication ...
🔥🔥🔥  90  L  35  I  90  src/middleware/basic-auth/index.ts  Broken checks can deny access or expose routes ...
🔥🔥   90  L   2  I  90  src/middleware/jwt/index.ts         JWT middleware exports and context typing may fail ...
```

Columns: tier, heat, `L` likelihood, `I` impact (`I~` is an estimate without the LLM). The last row is stable (likelihood 2) but impactful, so it is HIGH (protect), not CRITICAL.

</details>

### Tour

A reading path for newcomers, auditors and agents learning the codebase. Packages come in dependency order (what everything else imports first), and within each level the most impactful code comes first (then the most central). Each stop has a `file:line` anchor at the first top-level declaration, its impact, likelihood and quadrant, why it matters (LLM critical reason or summary, else static signals), what it depends on, what uses it, and owners from recent commits and annotations. No LLM calls.

```sh
highstakes get tour --per-component 2                   # 2 most important files of every crate/package/module
highstakes get tour --per-component 2 --min-impact 80   # Only components that reach impact 80
highstakes get tour --limit 10                          # 10 most impactful files repo-wide, dependencies first
highstakes get tour --focus internal/llm                # One directory
highstakes get tour --json > tour.json                  # Feed /show-me walkthrough or an agent
```

<details><summary><b>Example: highstakes get tour --limit 3</b></summary>

```
$ highstakes get tour --limit 3        # hono
Tour: 3 stops, dependencies first

1. src/middleware/basic-auth  [level 2, max impact 90, CRITICAL]
   1. src/middleware/basic-auth/index.ts:12  🔥🔥🔥 critical  impact 90, likelihood 35, fragile-critical
      why: Authentication security boundary
      depends on: src/context.ts, src/utils/basic-auth.ts, src/http-exception.ts, src/types.ts, src/utils/buffer.ts

2. src/middleware/bearer-auth  [level 2, max impact 90, CRITICAL]
   2. src/middleware/bearer-auth/index.ts:16  🔥🔥🔥 critical  impact 90, likelihood 38, fragile-critical
      why: Security boundary for bearer-protected endpoints

3. src/utils/jwt  [level 3, max impact 92, CRITICAL]
   3. src/utils/jwt/jws.ts:13  🔥🔥🔥 critical  impact 92, likelihood 30, fragile-critical
      why: JWT cryptographic security boundary
      used by: src/middleware/jwk/jwk.ts, src/utils/jwt/jwt.ts, src/middleware/jwt/jwt.ts, src/utils/jwt/types.ts
```

</details>

<details><summary><b>Example: highstakes get tour --per-component 1 (two components shown)</b></summary>

```
$ highstakes get tour --per-component 1 --components crates/openshell-sandbox,crates/openshell-supervisor-network   # NVIDIA/OpenShell
Tour: 2 stops, dependencies first

1. crates/openshell-sandbox  [level 4, max impact 95, CRITICAL]
   1. crates/openshell-sandbox/src/boundary_server.rs:15  🔥🔥🔥 critical  impact 95, likelihood 42, fragile-critical
      why: Enforces sandbox isolation and authorized control
      depends on: crates/openshell-sandbox-backend/src/boundary_protocol.rs, crates/openshell-sandbox/src/lib.rs, crates/openshell-sandbox-backend/src/sandbox_auth.rs, crates/openshell-sandbox/src/boundary_io.rs, crates/openshell-sandbox/src/network_broker.rs (+12 more)
      used by: crates/openshell-sandbox/src/lib.rs

2. crates/openshell-supervisor-network  [level 5, max impact 92, CRITICAL]
   2. crates/openshell-supervisor-network/src/proxy.rs:6  🔥🔥🔥 critical  impact 92, likelihood 50, fragile-critical
      why: Enforces sandbox network access and identity-bound policy
      depends on: crates/openshell-core/src/secrets.rs, crates/openshell-supervisor-network/src/l7/tls.rs, crates/openshell-supervisor-network/src/proxy/destination.rs, crates/openshell-supervisor-network/src/lib.rs, crates/openshell-supervisor-network/src/opa.rs (+18 more)
      used by: crates/openshell-supervisor-network/src/proxy/destination.rs, crates/openshell-supervisor-network/src/run.rs, crates/openshell-supervisor-network/src/lib.rs, crates/openshell-supervisor-network/src/proxy/relay.rs, crates/openshell-supervisor-network/src/host.rs (+4 more)
```

A component is a Rust crate, JS/TS package, Python package or Go module (the shallowest directory with its marker file), else the top two segments under `cmd/`, `internal/` and `pkg/`, else the top-level directory. See [the CLI reference](docs/cli-reference.md#highstakes-get-tour).

</details>

The tour is a starting draft. Add expert guidance through the `annotations` map in `.heatmap/heatmap.json` (`owner`, `notes`); both appear on each stop.

### PR Risk

```sh
highstakes pr check                                     # Score diff vs main
highstakes pr check --base dev --json                   # For CI pipelines
highstakes pr check --fail-on high                      # Exit 5 on high/critical PRs
highstakes pr check --shadow                            # Log the decision, never fail
```

`pr check` reports two numbers: importance (the highest impact among the touched files, or heat where there is no LLM assessment) and change risk (size, new files, config or infra files, deletions in hot files, test ratio). It also names suggested reviewers: the most active recent authors of the hottest changed files, excluding the PR author. See the [CLI reference](docs/cli-reference.md#highstakes-pr-check).

### Review Policy

```sh
highstakes create codeowners                            # Print a CODEOWNERS block for HIGH/CRITICAL paths
highstakes create codeowners --write                    # Update the marked block in .github/CODEOWNERS
highstakes create policy > .github/highstakes-policy.yaml   # No-auto-approve paths for AI reviewers
highstakes create policy --json --min-tier critical
```

Copilot and CodeRabbit can approve PRs, limited by path rules. `create policy` generates those rules from the heatmap so bots stay out of high-stakes code (by tier, and by LLM impact so stable but critical code is covered too), and `create codeowners` routes the same paths to the people who know them. Map git emails to GitHub handles with the `owners` key in [configuration](docs/configuration.md#owner-aliases).

### Incidents

```sh
highstakes incident create --file src/auth.rs \
  --severity high --description "Token bypass"
highstakes incident list
```

### Calibration

Is the heatmap right? `check calibration` replays git history and compares the scores with what actually broke: reverts, bug-fix commits, security fixes (CVE, GHSA, "security", "vuln") and recorded incidents.

```sh
highstakes check calibration                    # Last 365 days, in-sample
highstakes check calibration --holdout 180d     # Score with history before the cutoff, count events after it
highstakes check calibration --top 20 --json    # Precision/recall for the 20 hottest files
highstakes check calibration --events security --since 1095d  # Security fixes only: a proxy for impact
highstakes create labels --out labels.csv       # Blind sheet for maintainers to rate impact 1-5
highstakes check calibration --labels labels.csv  # Score blast radius and heat against their ratings
```

It reports the event rate per tier, precision@K and recall@K of the K hottest files, lift over the base rate (1.0 means no better than random), Spearman rank correlation, and the same metrics for the two axes (likelihood, impact) and for blast radius, churn, complexity and centrality alone, so you can see which signal predicts fixes. It also lists surprises: LOW-tier files with many fixes and CRITICAL/HIGH files with none.

Churn comes from the same history as the fixes, so the default run flatters it. Use `--holdout` for an honest number. Blast radius measures impact if broken, not how often code breaks, so a weak correlation for it is expected; check it with `--events security` or maintainer labels instead. See [How It Works](docs/how-it-works.md#is-it-right).

To judge a scoring change across projects and languages, run the pinned multi-repo benchmark in [bench/](bench/README.md).

## CI / PR Integration

Run `highstakes github install` to generate the workflow, or add it manually:

<details><summary><b>GitHub Action: PR triage comments</b></summary>

```yaml
# .github/workflows/heatmap-triage.yml
name: HighStakes Triage
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: read

jobs:
  triage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Install heatmap
        run: go install github.com/zanetworker/highstakes/cmd/highstakes@latest

      - name: Analyze and check PR
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
        run: |
          highstakes init
          highstakes analyze
          highstakes pr check --base origin/${{ github.base_ref }} --json > pr-risk.json

      - name: Post risk comment
        uses: actions/github-script@v7
        with:
          script: |
            const risk = JSON.parse(require('fs').readFileSync('pr-risk.json','utf8'));
            const e = {critical:'🔥🔥🔥',high:'🔥🔥',medium:'🔥',low:'🟢'};
            let body = '## '+e[risk.tier]+' HighStakes: '+risk.tier.toUpperCase()+'\n\n';
            body += '| File | Score | Tier | Lines |\n|------|-------|------|-------|\n';
            for (const f of risk.files_changed.sort((a,b)=>b.heat_score-a.heat_score))
              body += '| '+f.path+' | '+f.heat_score+' | '+e[f.tier]+' '+f.tier+' | +'+f.lines_added+'/-'+f.lines_deleted+' |\n';
            body += '\n**Review:** '+risk.review_requirements.min_reviewers+' reviewers';
            if (risk.review_requirements.requires_senior) body += ' (senior)';
            body += ', auto-merge: '+(risk.review_requirements.auto_merge?'✅':'❌')+'\n';
            const comments = await github.rest.issues.listComments({
              owner:context.repo.owner, repo:context.repo.repo, issue_number:context.issue.number});
            const existing = comments.data.find(c=>c.body.includes('HighStakes:'));
            if (existing) await github.rest.issues.updateComment({
              owner:context.repo.owner, repo:context.repo.repo, comment_id:existing.id, body});
            else await github.rest.issues.createComment({
              owner:context.repo.owner, repo:context.repo.repo, issue_number:context.issue.number, body});
```

</details>

<details><summary><b>Block merge on HIGH/CRITICAL files</b></summary>

Add a required status check that fails when the PR touches high-risk files:

```yaml
      - name: Gate on risk tier
        run: |
          TIER=$(jq -r .tier pr-risk.json)
          if [ "$TIER" = "critical" ] || [ "$TIER" = "high" ]; then
            echo "::error::PR touches $TIER-tier files. Requires senior review before merge."
            exit 1
          fi
```

Then in **Settings > Branches > Branch protection rules**, add `triage` as a required status check. PRs touching critical or high files cannot merge until the check passes (i.e., a senior reviewer approves and the step is skipped or overridden).

</details>

<details><summary><b>Use in any CI (GitLab, Jenkins, etc.)</b></summary>

The CLI is the interface. Any CI that can run a binary works:

```sh
# Install
go install github.com/zanetworker/highstakes/cmd/highstakes@latest

# Analyze (uses cache, only re-assesses changed files)
export OPENROUTER_API_KEY="$OPENROUTER_KEY"
highstakes init && highstakes analyze

# Check PR risk (exit code reflects tier)
highstakes pr check --base origin/main --json > risk.json

# Use the output
TIER=$(jq -r .tier risk.json)
SCORE=$(jq -r .heat_score risk.json)
FILES=$(jq -r '.files_changed | length' risk.json)

echo "PR risk: $TIER ($SCORE), $FILES files changed"

# Fail pipeline on high/critical
if [ "$TIER" = "critical" ] || [ "$TIER" = "high" ]; then
  echo "Requires human review"
  exit 1
fi
```

</details>

**What the PR comment looks like:**

```markdown
## 🔥🔥 HighStakes: HIGH

| File | Heat | Tier | Lines |
|------|------|------|-------|
| ⚠️ src/auth/oidc.rs | 63 | 🔥🔥 HIGH | +45/-12 |
| ⚠️ src/sandbox/proxy.rs | 63 | 🔥🔥 HIGH | +8/-3 |
| ✓ src/tui/theme.rs | 6 | 🟢 LOW | +2/-1 |

### ⚠️ Circuit Breaker Signals
- Large diff (>500 lines)

### Review Requirements
- **Reviewers**: 2 (senior)
- **Security Scan**: Required
- **Auto-Merge**: ❌ Blocked
- **Est. Review Time**: 60 min
```

### Agent Introspection

```sh
highstakes agent-context
```

Machine-readable JSON of all commands, flags, exit codes, and models. Built for AI agents to discover the CLI surface programmatically.

## Models

One [OpenRouter](https://openrouter.ai) API key, any model. Assessments cached by content hash; re-runs only re-assess changed files.

| Model | Cost / 500 files | Best For |
|-------|-----------------|----------|
| `openai/gpt-6-luna` | ~$0.19 | Default. Best impact ranking on the benchmark |
| `z-ai/glm-5.3-flash` | ~$0.04 | Cheapest; 2-3x slower |
| `deepseek/deepseek-v4.1-flash` | ~$0.13 | Open weights, can be self-hosted |
| `anthropic/claude-haiku-5.5` | ~$0.19 | Zero failed calls on the benchmark |

Chosen on the [calibration benchmark](bench/README.md) (7 repos, 6,240 files, October 2026): the four models rank files by impact in largely the same order (rank correlation 0.72 to 0.90), so a cheap model is enough. Costs are estimates from token counts.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Internal error |
| `2` | Invalid input |
| `3` | External dependency failure |
| `4` | Not found |
| `5` | PR blocked by `pr check --fail-on` |

## Requirements

- Go 1.25+
- Git
- `OPENROUTER_API_KEY` (optional with `--no-llm`)

## Related

- [Agentic Code Review](https://addyo.substack.com/p/agentic-code-review) by Addy Osmani
- [Semantically-Seeded Impact Analysis](https://arxiv.org/abs/2606.18855) (Jun 2026)
- [BitsAI-CR: Two-Stage Code Review at ByteDance](https://arxiv.org/abs/2501.15134)
- [c-CRAB: Code Review Agent Benchmark](https://arxiv.org/abs/2603.23448)
- [OpenSSF Criticality Score](https://openssf.org/projects/criticality-score/)

## License

MIT
