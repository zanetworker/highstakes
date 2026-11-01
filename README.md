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
| **High impact (top 20% of the repo, at least 61)** | **Protect** (HIGH): stable but costly if broken; keep owners on review, never auto-merge | **Fragile and critical** (CRITICAL): senior review, add tests first |
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

**Architecture**: one node per package (directory), laid out in dependency layers: entry points on top, foundations at the bottom. Node size is total lines, color is the highest impact in the package (what breaks if it breaks), and a dashed outline with an "N likely" badge marks packages holding files likely to need fixes. Each node names its hottest file. Arrows run from importer to dependency and carry the number of importing files. Click a package to list its files hottest-first in the detail panel, then click a file for its blast radius. Click an arrow to see the file-level imports behind it (for example `api/src/routes.rs -> core/src/policy.rs`). Packages outside the current tier filter or search are dimmed. Wide layers wrap into extra rows; the zoom buttons, **Fit** (whole graph on screen), mouse wheel, trackpad pinch and drag-to-pan handle large repos.

**Group by: Unit** switches the Architecture view from packages to runtime units: one box per deployable entry point (a Rust binary, a Go `main` package, a `package.json` bin or start script, a Python script), holding the components only that unit links, with shared libraries in a band below. Boxes are colored by max impact, carry the fragility marker, and show placement inferred from Dockerfiles, Helm values and Kubernetes manifests. A sandbox runtime and its supervisor built as two binaries show as two boxes, each with the libraries only it links. Click a unit for its entry point, placement evidence, components and files (impact, likelihood, quadrant). Supporting units (examples, scripts, helper bins) stay hidden until **Supporting units** is on, and links declared in `.heatmap/architecture.yaml` draw dashed. Units come from imports and manifests, not from a running system; see [How It Works](docs/how-it-works.md#runtime-units).

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
highstakes                                              # Terminal TUI
```

### Query

```sh
highstakes get <file>                                   # Why impact, why likely, what to do
highstakes list --tier high --limit 10                  # Filter files
highstakes list --sort impact --limit 10                # Most impactful first (also: likelihood, heat)
highstakes list units                                   # Binaries and scripts, what each links, where it is packaged
highstakes get architecture --out arch.json             # Generated architecture diagram (SeeCode spec)
highstakes report                                       # Distribution report
```

All commands support `--json` for machine-readable output.

<details open><summary><b>Example: highstakes get</b></summary>

`get` says why a file is impactful, why it is likely to break and what to do, quoting the file's numbers and where they rank in the repo:

```
$ highstakes get src/utils/jwt/jwt.ts        # honojs/hono, owners trimmed

src/utils/jwt/jwt.ts  🔥🔥🔥 CRITICAL  (score: 92)

FRAGILE AND CRITICAL (impact 92, likelihood 37; impactful starts at 61, likely at 20 in this repo)
Why impact is high: security 92 · user 75 · availability 65
  "JWT signing and verification failures can disrupt authentication across dependent services."
  "JWT verification is a security boundary"
Why it's likely to break:
  18 authors overall (top 4% of this repo)
  9 functions, cyclomatic 70 in one file (top 4%)
  5 files import it (top 14%)
  4 recent fixes; latest: "fix(jwt): throw JwtTokenInvalid when the signature is not valid base64ur..." (2026-09-20)
What to do:
  • Every PR: senior review and sign-off from a top owner (@calloc134, ...)
  • Add to CODEOWNERS; never auto-merge
  • Split it: cyclomatic 70 in one file
  • Add regression tests around the 4 recent fixes
  • Spread knowledge: 18 authors change it; document its invariants and keep an owner on review

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

A reading path for newcomers, auditors and agents learning the codebase. The most important product code comes first (highest impact, then the most fragile-critical files), and whatever it depends on is read just before it. Examples, scripts, CI, deployment manifests, docs, tests, benchmarks and vendored code are supporting components: hidden by default, shown in a final part with `--include-supporting`. Each stop has a `file:line` anchor at the first top-level declaration, its impact, likelihood and quadrant, why it matters (LLM critical reason or summary, else static signals), what it depends on, what uses it, and owners from recent commits and annotations. No LLM calls.

```sh
highstakes get tour --per-component 1 --min-impact 85   # 1 file per crate/package/module, only files with impact 85+
highstakes get tour --per-component 2 --include-supporting  # Add examples, scripts, CI and deploy in a final part
highstakes get tour --limit 10                          # 10 most impactful files repo-wide
highstakes get tour --focus internal/llm                # One directory
highstakes get tour --json > tour.json                  # Feed /show-me walkthrough or an agent
```

A component is a Rust crate, JS/TS package, Python package or Go module (the shallowest directory with its marker file), else the top two segments under `cmd/`, `internal/` and `pkg/`, else the top-level directory. See [the CLI reference](docs/cli-reference.md#highstakes-get-tour).

`--format walkthrough --out tour.data.json` writes the same tour as data for the show-me rich step-through (see [Generated Pictures and Walkthroughs](#generated-pictures-and-walkthroughs)).

The tour is a starting draft. Add expert guidance through the `annotations` map in `.heatmap/heatmap.json` (`owner`, `notes`); both appear on each stop.

### Generated Pictures and Walkthroughs

Hand-drawn architecture diagrams drift and get topology wrong (a sandbox runtime and its supervisor drawn as one box when they run in separate pods). HighStakes generates the picture and the walkthrough from the heatmap instead, and every box links to its files.

```sh
highstakes get architecture --format seecode --out arch.json      # SeeCode architecture spec: top units, imports, placements
node <seecode skill>/scripts/seecode.mjs render arch.json          # arch.html: animated, explorable diagram
highstakes get architecture --json                                 # Same diagram as plain data
highstakes get tour --per-component 1 --min-impact 85 --format walkthrough --out tour.data.json
python3 <show-me skill>/scripts/walkthrough_build.py tour.data.json   # tour.html: step-through, one step per stop
```

The architecture spec holds the top product units by max impact (9 boxes by default, the rest folded into one "other units" box), the library most units import, import arrows labelled with importing-file counts, placement groups, and per box its top 3 files with quadrant and first action as evidence. Supporting units (examples, scripts, tests, helper bins) are left out unless `--include-supporting`.

Imports cannot see runtime links such as a gRPC session between two binaries. Declare those, with evidence, in `.heatmap/architecture.yaml`; they draw dashed and marked "declared", apart from what was inferred:

```yaml
links:
  - {from: worker, to: api, label: gRPC job stream, both: true,
     evidence: "docs/architecture.md:42"}
placement:
  - {unit: worker, runs_in: worker pod,
     evidence: "deploy/helm/app/templates/worker.yaml:12"}
```

See [the CLI reference](docs/cli-reference.md#highstakes-get-architecture) and [Configuration](docs/configuration.md#architecture-declarations).

### Case study: a multi-pod Rust project (NVIDIA/OpenShell)

[NVIDIA/OpenShell](https://github.com/NVIDIA/OpenShell) is a Rust workspace of 39 crates that ships several binaries into separate pods: a gateway, a supervisor, a sandbox runtime and compute drivers, plus helper binaries. It shows what runtime units, the tour and architecture declarations add on a repo where the deployment shape is not obvious from the directory tree.

On this repo the LLM scores impact generously, so a fixed bar would call about a third of the files impactful. The repo-relative cutoff puts "impactful" at 78 instead of 61, which leaves 108 CRITICAL and 146 HIGH files out of 1,567 (was 178 and 353 with the fixed bar).

<details><summary><b>Example: highstakes list units</b></summary>

```
$ highstakes list units        # NVIDIA/OpenShell, trimmed

openshell-supervisor  [rust-bin]  crates/openshell-supervisor/src/main.rs (crates/openshell-supervisor/Cargo.toml:13)
  impact 95  41 fragile-critical  242 files
  own: crates/openshell-supervisor, crates/openshell-otel-test-support, crates/openshell-supervisor-network, crates/openshell-supervisor-process
  links units: openshell-driver-vm (crates/openshell-driver-vm)
  shared: crates/openshell-core, crates/openshell-policy, crates/openshell-sandbox-backend, ...
  inferred placement: ENTRYPOINT of deploy/docker/Dockerfile.supervisor [deploy/docker/Dockerfile.supervisor:18]
  inferred placement: helm chart deploy/helm/openshell: supervisor image openshell/supervisor [deploy/helm/openshell/values.yaml:63]

openshell-sandbox  [rust-bin]  crates/openshell-sandbox/src/main.rs (crates/openshell-sandbox/Cargo.toml:13)
  impact 95  22 fragile-critical  177 files
  own: crates/openshell-sandbox
  shared: crates/openshell-core, crates/openshell-sandbox-backend, ...
  inferred placement: ENTRYPOINT of deploy/docker/Dockerfile.sandbox [deploy/docker/Dockerfile.sandbox:22]
  inferred placement: helm chart deploy/helm/openshell: sandboxRuntime image openshell/sandbox [deploy/helm/openshell/values.yaml:49]
```

`own` is code only this unit links, `links units` is another binary's crate it links in-process, `shared` is linked by two or more units. Units are sorted by max impact.

</details>

<details><summary><b>Example: highstakes get tour --per-component 1 --min-impact 85 (first three sections, neighbour and owner lines trimmed)</b></summary>

```
$ highstakes get tour --per-component 1 --min-impact 85   # NVIDIA/OpenShell
Tour: 27 stops across 27 product components, dependencies first
7 supporting components hidden (--include-supporting shows them)

1. crates/openshell-crypto  [level 0, max impact 90, HIGH]
   1. crates/openshell-crypto/src/aws_lc.rs:12  🔥🔥 high  impact 90, likelihood 23, protect
      why: Core cryptographic security boundary
      likely because: no notable churn, complexity, importers or fixes
      do: Owner review on every PR (varshaprasad96@gmail.com); Add to CODEOWNERS; never auto-merge

2. crates/openshell-extension-core  [level 0, max impact 85, HIGH]
   2. crates/openshell-extension-core/src/auth.rs:12  🔥🔥 high  impact 85, likelihood 21, protect
      why: Bearer-token authentication boundary
      likely because: recent fix: "fix(security): authenticate extension services (#2638)" (2026-08-14)

3. crates/openshell-core  [level 1, max impact 90, CRITICAL]
   3. crates/openshell-core/src/secrets.rs:5  🔥🔥🔥 critical  impact 90, likelihood 39, fragile-critical
      why: Secret resolution and credential-leak prevention boundary
      likely because: 7 commits in the last 90 days, 9 authors overall (top 18% of this repo)
```

The first sections are the foundations that `crates/openshell-sandbox-backend` (impact 95, section 5) builds on. `deploy`, `scripts`, `.github`, `e2e` and `examples` appear only with `--include-supporting`, after section 27.

</details>

With its runtime links declared in `.heatmap/architecture.yaml`, `get architecture` shows the supervisor in its own pod talking to the gateway (control session) and to the sandbox runtime in the workload pod (boundary protocol), the gateway calling four compute drivers, and all of them importing `openshell-core`.

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

One [OpenRouter](https://openrouter.ai) API key, any model. Assessments are cached by file content, model and prompt version: re-runs only re-assess changed files, and switching `--model` or upgrading to a HighStakes release with a new prompt version re-assesses everything.

| Model | Cost / 500 files | Best For |
|-------|-----------------|----------|
| `openai/gpt-6-luna` | ~$0.19 | Default. Best impact ranking on the benchmark |
| `z-ai/glm-5.3-flash` | ~$0.04 | Cheapest; 2-3x slower |
| `deepseek/deepseek-v4.1-flash` | ~$0.13 | Open weights, can be self-hosted |
| `anthropic/claude-haiku-5.5` | ~$0.19 | Zero failed calls on the benchmark |

Chosen on the [calibration benchmark](bench/README.md) (7 repos, about 5,500 files assessed per model, October 2026): the four models rank files by impact in largely the same order (rank correlation 0.72 to 0.90), so a cheap model is enough. Costs are upper estimates from token counts.

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
