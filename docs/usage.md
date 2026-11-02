---
layout: default
title: Usage guide
---

# Usage guide

Common tasks with real output. Every flag is in the [CLI reference](cli-reference.md).

## Analyze

```sh
highstakes init                                         # Create .heatmap/ config
highstakes analyze                                      # Full LLM analysis (~$0.15)
highstakes analyze --model z-ai/glm-5.2                 # Different model
highstakes analyze --no-llm                             # Static only, no API key
```

## Visualize

```sh
highstakes dashboard                                    # HTML dashboard (browser)
highstakes                                              # Terminal TUI
```

## Query

```sh
highstakes get <file>                                   # Verdict, both questions, before-merge and backlog checklists
highstakes list --tier high --limit 10                  # Filter files
highstakes list --sort impact --limit 10                # Most impactful first (also: likelihood, heat)
highstakes list units                                   # Binaries and scripts, what each links, where it is packaged
highstakes list tasks                                   # Backlog items agents may take on (autonomy level 2+), with "done when"
highstakes get architecture --out arch.json             # Generated architecture diagram (SeeCode spec)
highstakes report                                       # Distribution report
```

All commands support `--json` for machine-readable output.

<details open><summary><b>Example: highstakes get</b></summary>

`get` gives the verdict, both questions with their repo cutoffs, what to do before the next merge, what to put on the backlog (with a copyable command), and why:

```
$ highstakes get crates/openshell-supervisor-network/src/proxy.rs     # NVIDIA/OpenShell, commands trimmed

crates/openshell-supervisor-network/src/proxy.rs
HARDEN NOW
Will it break?         50  HIGH  repo cutoff 35
How bad if it breaks?  92  HIGH  repo cutoff 78
Scores are a relative signal within this repo, not a probability.
Will it break? is validated on bug-fix history; How bad if it breaks? is AI-assessed, not yet checked against maintainer ratings.

Do before the next merge
  [ ] Get sign-off from a senior owner (@johntmyers, @second-owner)
      why: Harden now: likely to break and costly when it does
  [ ] Add regression tests around the 4 recent fixes
      why: 4 recent fixes; latest: "fix(network): align destination checks for direct connections (#5004)" (2026-10-28)
  [ ] Add contract tests for its public interface
      why: 11 files import it (top 7%)
  [ ] Keep auto-merge off
      why: Harden now files never auto-merge
Put on the backlog
  [ ] Split it into smaller files
      why: 479 functions in one file (top 1%)
      gh issue create --title 'Split crates/openshell-supervisor-network/src/proxy.rs' --body '...'
  [ ] Add a CODEOWNERS line
      why: not covered by CODEOWNERS
      /crates/openshell-supervisor-network/src/proxy.rs @johntmyers @second-owner
  [ ] Document its invariants
      why: 28 authors change it (top 2%)
      gh issue create --title 'Document the invariants of crates/...' --body '...'

Why
  Break signals: 55 commits in the last 90 days, 28 authors overall (top 1% of this repo); 479 functions, cyclomatic 1092 in one file (top 1%); ...
  Break cost: security 92 · availability 85 · user 85 · data 70; "Core sandbox egress proxy; bugs can block traffic or bypass network policy."
Latest fix  2026-10-28  fix(network): align destination checks for direct connections (#5004)
```

Reasons and actions are deterministic (no extra LLM call); see [How It Works](how-it-works.md#reasons-and-actions) for the rules. `pr check` turns the before-merge items of the Harden now and Protect files a PR touches into one blocking checklist and ticks the ones the PR already satisfies; items for Stabilize files go into a folded "Worth doing (does not block)" section. Under the verdict, the comment says whether AI may auto-merge, with the reason (`🤖 AI may auto-merge: ❌ no (touches Protect files)`), from the [autonomy levels](cli-reference.md#agent-autonomy). The dashboard and TUI detail panels show the same sections.

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

## Tour

A reading path for newcomers, auditors and agents learning the codebase. The most important product code comes first (highest impact, then the most Harden now files), and whatever it depends on is read just before it. Examples, scripts, CI, deployment manifests, docs, tests, benchmarks, test data and vendored code are supporting components: hidden by default, shown in a final part with `--include-supporting`. Each stop has a `file:line` anchor at the first top-level declaration, its verdict, both questions with the repo cutoffs, why it matters, the top before-merge and backlog action (LLM critical reason or summary, else static signals), what it depends on, what uses it, and owners from recent commits and annotations. No LLM calls.

```sh
highstakes get tour --per-component 1 --min-impact 85   # 1 file per crate/package/module, only files with impact 85+
highstakes get tour --per-component 2 --include-supporting  # Add examples, scripts, CI and deploy in a final part
highstakes get tour --limit 10                          # 10 most impactful files repo-wide
highstakes get tour --focus internal/llm                # One directory
highstakes get tour --json > tour.json                  # Feed /show-me walkthrough or an agent
```

A component is a Rust crate, JS/TS package, Python package or Go module (the shallowest directory with its marker file), else the top two segments under `cmd/`, `internal/` and `pkg/`, else the top-level directory. See [the CLI reference](cli-reference.md#highstakes-get-tour).

`--format walkthrough --out tour.data.json` writes the same tour as data for the show-me rich step-through (see [Generated Pictures and Walkthroughs](#generated-pictures-and-walkthroughs)).

The tour is a starting draft. Add expert guidance through the `annotations` map in `.heatmap/heatmap.json` (`owner`, `notes`); both appear on each stop.

## Generated Pictures and Walkthroughs

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

See [the CLI reference](cli-reference.md#highstakes-get-architecture) and [Configuration](configuration.md#architecture-declarations).

## Case study: a multi-pod Rust project (NVIDIA/OpenShell)

[NVIDIA/OpenShell](https://github.com/NVIDIA/OpenShell) is a Rust workspace of 39 crates that ships several binaries into separate pods: a gateway, a supervisor, a sandbox runtime and compute drivers, plus helper binaries. It shows what runtime units, the tour and architecture declarations add on a repo where the deployment shape is not obvious from the directory tree.

On this repo the LLM scores impact generously, so a fixed bar would call about a third of the files impactful. The repo-relative cutoff puts "impactful" at 78 instead of 61, which leaves 108 CRITICAL and 146 HIGH files out of 1,567 (was 178 and 353 with the fixed bar).

<details><summary><b>Example: highstakes list units</b></summary>

```
$ highstakes list units        # NVIDIA/OpenShell, trimmed

openshell-supervisor  [rust-bin]  crates/openshell-supervisor/src/main.rs (crates/openshell-supervisor/Cargo.toml:13)
  impact 95  41 Harden now  242 files
  own: crates/openshell-supervisor, crates/openshell-otel-test-support, crates/openshell-supervisor-network, crates/openshell-supervisor-process
  links units: openshell-driver-vm (crates/openshell-driver-vm)
  shared: crates/openshell-core, crates/openshell-policy, crates/openshell-sandbox-backend, ...
  inferred placement: ENTRYPOINT of deploy/docker/Dockerfile.supervisor [deploy/docker/Dockerfile.supervisor:18]
  inferred placement: helm chart deploy/helm/openshell: supervisor image openshell/supervisor [deploy/helm/openshell/values.yaml:63]

openshell-sandbox  [rust-bin]  crates/openshell-sandbox/src/main.rs (crates/openshell-sandbox/Cargo.toml:13)
  impact 95  22 Harden now  177 files
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

1. crates/openshell-crypto  [level 0, max how bad 90, Protect]
   1. crates/openshell-crypto/src/aws_lc.rs:12  Protect
      Will it break? 23 LOW, cutoff 35 · How bad if it breaks? 90 HIGH, cutoff 78
      why: Core cryptographic security boundary
      break signals: no notable churn, complexity, importers or fixes
      before merge: Get an owner's review (@crypto-owner)
      backlog: Add a CODEOWNERS line

2. crates/openshell-extension-core  [level 0, max how bad 85, Protect]
   2. crates/openshell-extension-core/src/auth.rs:12  Protect
      Will it break? 21 LOW, cutoff 35 · How bad if it breaks? 85 HIGH, cutoff 78
      why: Bearer-token authentication boundary
      break signals: recent fix: "fix(security): authenticate extension services (#2638)" (2026-08-14)

3. crates/openshell-core  [level 1, max how bad 90, Harden now]
   3. crates/openshell-core/src/secrets.rs:5  Harden now
      Will it break? 39 HIGH, cutoff 35 · How bad if it breaks? 90 HIGH, cutoff 78
      why: Secret resolution and credential-leak prevention boundary
      break signals: 7 commits in the last 90 days, 9 authors overall (top 18% of this repo)
```

The first sections are the foundations that `crates/openshell-sandbox-backend` (impact 95, section 5) builds on. `deploy`, `scripts`, `.github`, `e2e` and `examples` appear only with `--include-supporting`, after section 27.

</details>

With its runtime links declared in `.heatmap/architecture.yaml`, `get architecture` shows the supervisor in its own pod talking to the gateway (control session) and to the sandbox runtime in the workload pod (boundary protocol), the gateway calling four compute drivers, and all of them importing `openshell-core`.

## PR Risk

```sh
highstakes pr check                                     # Verdict and before-merge checklist for the diff vs main
highstakes pr check --format markdown                   # The PR comment the triage workflow posts
highstakes pr check --approvals ana,bo                  # Tick owner-review items these approvers satisfy
highstakes pr check --base dev --json                   # For CI pipelines
highstakes pr check --fail-on critical                  # Exit 5 while a Harden now PR has open before-merge items
highstakes pr check --fail-on critical --signed-off     # A maintainer added the highstakes:signed-off label
highstakes pr check --shadow                            # Log the decision, never fail
```

`pr check` leads with the verdict of the most severe changed file (Harden now, Protect, Stabilize, Routine) and whether AI may auto-merge it, then one checklist of the before-merge items of the Harden now and Protect files it touches; Stabilize work (refactor the touched function, regression tests) is listed as worth doing, not blocking. It ticks the items the PR already satisfies: a test next to a risky file was added or changed, not deleted ("add tests" items), CODEOWNERS already covers the path, a suggested owner approved (`--approvals`, filled from the GitHub API in CI), a maintainer added the `highstakes:signed-off` label (`--signed-off`), or auto-merge is off (`--auto-merge-enabled false`). Harden now files also get "ask for a smaller PR" when the diff is over 500 lines or spans 3+ components. Backlog items (split the file, a CODEOWNERS line, a test-gap issue) come with a copyable command. It also reports change risk and suggested reviewers. See the [CLI reference](cli-reference.md#highstakes-pr-check).

## Review Policy

```sh
highstakes create codeowners                            # Print a CODEOWNERS block for HIGH/CRITICAL paths (@handles only)
highstakes create codeowners --write                    # Update the marked block in .github/CODEOWNERS
highstakes create policy > .github/highstakes-policy.yaml   # No-auto-approve paths for AI reviewers
highstakes create policy --json --min-tier critical
highstakes create policy --agents --out .highstakes/autonomy.yaml   # Agent autonomy: path globs per level
```

Copilot and CodeRabbit can approve PRs, limited by path rules. `create policy` generates those rules from the heatmap so bots stay out of high-stakes code (by tier, and by LLM impact so stable but impactful code is covered too), and `create codeowners` routes the same paths to the people who know them. `create policy --agents` goes further for coding agents: every path gets a level, 3 auto-merge (Routine), 2 auto-fix with a human merging (Stabilize), 1 propose only (Protect), 0 assist only (Harden now). Levels are conservative because "How bad?" is AI-assessed: files without an LLM assessment are capped at 1, and `autonomy.max_level` in the config caps everything. `pr check` reports the PR's level (`auto_merge_allowed` in JSON) and `list tasks` turns the backlog into an agent work queue. See [Agent autonomy](cli-reference.md#agent-autonomy). Map git emails to GitHub handles with the `owners` key in [configuration](configuration.md#owner-aliases).

## Incidents

```sh
highstakes incident create --file src/auth.rs \
  --severity high --description "Token bypass"
highstakes incident list
```

## Calibration

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

Churn comes from the same history as the fixes, so the default run flatters it. Use `--holdout` for an honest number. Blast radius measures impact if broken, not how often code breaks, so a weak correlation for it is expected; check it with `--events security` or maintainer labels instead. See [How It Works](how-it-works.md#is-it-right).

To judge a scoring change across projects and languages, run the pinned multi-repo benchmark in [bench/](../bench/README.md).

## The PR comment

**What the PR comment looks like** (a `pr check --format markdown` on NVIDIA/OpenShell after the PR also changed a test next to `proxy.rs`, with `--repo NVIDIA/OpenShell`; issue URLs and reasons shortened here; GitHub folds the `<details>` blocks):

```markdown
## ⛔ Harden before merge

⛔ 2 harden · ✅ 2 routine

🤖 AI may auto-merge: ❌ no (touches Harden now files)

### ✅ Do before merge (2 of 4 done)

- [ ] 👀 Get sign-off from @johntmyers on `crates/openshell-supervisor-network/src/proxy.rs` (a maintainer adds the `highstakes:signed-off` label after reviewing)
- [x] 🧪 Add regression tests around the 4 recent fixes (`crates/openshell-supervisor-network/src/proxy.rs`)
- [x] 🧪 Add contract tests for its public interface (`crates/openshell-supervisor-network/src/proxy.rs`)
- [ ] 🚫 Keep auto-merge off

|    | File | If it breaks | Breaks often? | Next step |
|---|---|---|---|---|
| ⛔ | `crates/openshell-supervisor-network/src/proxy.rs` | Core sandbox egress proxy; bugs can block traffic or bypass network policy. | 🔴 4 recent fixes | 👀 senior sign-off · [📝 open issue](https://github.com/NVIDIA/OpenShell/issues/new?title=Split...) |
| ⛔ | `crates/openshell-supervisor-network/src/egress_audit.rs` | new file: treated like its directory (harden now) | 🆕 new file | 👀 senior sign-off |
| ✅ | 2 other files | limited damage | 🟢 rarely | normal review |

### ⚡ One-click actions

- [📝 Open issue: Split crates/openshell-supervisor-network/src/proxy.rs into smaller files](https://github.com/NVIDIA/OpenShell/issues/new?title=Split...)
  <details><summary>What's in this issue</summary>

  - **Why it matters:** Core sandbox egress proxy; bugs can block traffic or bypass network policy. · Top impact: security 92 · availability 85
  - **Why now:** Harden now: likely to break and costly if it does; Will it break? 50 (high, cutoff 33); How bad if it breaks? 92 (high, cutoff 78); Recent fixes: "fix(proxy): ..." (2026-10-28); ...
  - **Suggested approach:** 1. Identify cohesive groups of the 479 functions (functions that share data or callers) 2. Move one group per PR into a new file in the same package 3. Keep the public API unchanged so importers need no edits 4. Run the tests after each move
  - **Done when:** the 479 functions of crates/openshell-supervisor-network/src/proxy.rs are spread over smaller files; tests pass
  - **Who / autonomy:** @johntmyers · L0 assist only

  </details>
- 🆕 Unscored new file(s), treated like their directory until the next analyze scores them: crates/openshell-supervisor-network/src/egress_audit.rs

<details><summary>🗺 Where this PR lands</summary>

(a Mermaid quadrant chart with numbered points, each inside its posture's quadrant, then a legend)

1. ⛔ `crates/openshell-supervisor-network/src/proxy.rs`: senior sign-off

</details>

<details><summary>Why these actions?</summary>
(per-file reasons, scores and repo cutoffs, suggested reviewers, worth doing, backlog commands, gate)
</details>
```

## Agent Introspection

```sh
highstakes agent-context
```

Machine-readable JSON of all commands, flags, exit codes, and models. Built for AI agents to discover the CLI surface programmatically.

## Related

- [Agentic Code Review](https://addyo.substack.com/p/agentic-code-review) by Addy Osmani
- [Semantically-Seeded Impact Analysis](https://arxiv.org/abs/2606.18855) (Jun 2026)
- [BitsAI-CR: Two-Stage Code Review at ByteDance](https://arxiv.org/abs/2501.15134)
- [c-CRAB: Code Review Agent Benchmark](https://arxiv.org/abs/2603.23448)
- [OpenSSF Criticality Score](https://openssf.org/projects/criticality-score/)
