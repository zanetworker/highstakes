# How It Works

## The Problem

Static analysis tools count branches, measure complexity, and trace imports. They can tell you that `auth.rs` has cyclomatic complexity 23. They cannot tell you that `auth.rs` handles OIDC token validation and that a bug in it means unauthorized access to every sandbox in the system.

Code Heatmap solves this by asking an LLM to read each file and answer: "if this code breaks, what's the blast radius?"

## Analysis Pipeline

```
highstakes analyze
  │
  ├── 1. Static Analysis
  │     ├── File discovery (respects .heatmap/config.yaml excludes; .github/ is included)
  │     ├── Language detection (Go, Python, Rust, TypeScript, Java, ..., plus YAML, TOML, JSON, Dockerfile, Terraform)
  │     ├── Complexity (cyclomatic + cognitive from a real parse: go/ast for Go, tree-sitter for Rust, TS/JS, Java, Kotlin, C#, C/C++, Ruby)
  │     ├── Dependency graph (imports resolved to repo files per language; reverse dependency count)
  │     └── File size (lines, bytes)
  │
  ├── 2. Git Analysis
  │     ├── Commit frequency (total, last 90 days)
  │     ├── Unique contributors
  │     ├── Recent changes (last 10 commits per file)
  │     └── Bug-fix detection (commit message heuristics)
  │
  ├── 3. LLM Blast Radius Assessment
  │     ├── Filter: skip tests, docs, lock files, generated files
  │     ├── Cache check: skip files with unchanged content, model and prompt version
  │     ├── Send file + path to LLM via OpenRouter
  │     ├── Parse structured JSON response
  │     └── Cache result by SHA-256 of model, prompt version and content
  │
  └── 4. Scoring
        ├── Likelihood: churn, complexity, centrality, path keywords, incidents
        ├── Impact: LLM blast radius (an estimate without the LLM)
        ├── Tier from the likelihood x impact quadrant (from heat without the LLM)
        ├── Calculate review requirements per tier
        └── Runtime units: entry points, what each links, inferred placement
```

## LLM Assessment

Each source file is sent to the LLM with a prompt asking it to score four impact dimensions on a 0-100 scale. The request asks for structured output (a strict JSON schema), with reasoning off and temperature 0 where the model allows, so answers parse reliably, repeat, and are not cut off by reasoning using the token budget. On OpenRouter it is routed only to providers that support every parameter; when a model's providers reject temperature or disabling reasoning, HighStakes drops those parameters (keeping the schema) and remembers the working request for the rest of the run. Other OpenAI-compatible endpoints (vLLM, Ollama) need `response_format` JSON schema support:

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

## Two Axes: Likelihood and Impact

HighStakes scores every file on two separate axes, because they answer different questions:

- **Likelihood** (0-100): how likely the file is to need fixes. It comes from history and structure, never from the LLM.
- **Impact** (0-100): how much breaks if the file has a bug. It is the LLM blast radius, the highest of the four dimensions.

The [calibration benchmark](../bench/README.md) showed why they stay apart. Churn and complexity predicted which files needed fixes later: with the weights below, the ten top-ranked files on the primary repos were all fixed after the holdout cutoff (precision@10 1.00, Spearman 0.43). Blast radius was consistent across four models (rank agreement 0.72 to 0.90) but correlated weakly with fixes (Spearman 0.18 in-sample and 0.23 holdout with the default model), because a stable auth module can have high impact without ever breaking. Blending blast radius into one score at 40% made fix prediction worse for every model (holdout Spearman 0.37 with the default model). So each axis keeps its meaning, and the tier comes from where a file sits on both.

### Likelihood

| Factor | Weight | Source |
|--------|--------|--------|
| Change Frequency | 40 | Commits in last 90 days (if the file has history) |
| Complexity | 30 | Cyclomatic + cognitive complexity |
| Incident History | 25 | Manual incident records (if any) |
| Dependency Centrality | 10 | How many files import this one (normalized 0-1) |
| Path Keywords | 5 | The larger of user impact and data sensitivity, both from path keywords |

Weights are relative and normalize over the factors present. They were chosen on the benchmark across seven repos in five languages: churn and complexity predicted later fixes best, centrality added a little, and test coverage is a placeholder today, so it carries no weight. Path keywords ("auth", "token", "payment", "user" and similar in the file path) are a weak signal: they match on names alone and cannot tell a core auth module from an SDK helper. They count once, as the larger of user impact and data sensitivity, so a file whose only signal is its name stays in the LOW tier. Likelihood is the same with or without the LLM.

### Impact

With the LLM, impact is the blast radius score (`impact_assessed: true`). Without it (`--no-llm`, no API key, or files the LLM skips such as docs and lock files), impact is an estimate from centrality and path keywords, weighted 10 and 5 as above, and `impact_assessed` is `false`. Treat the estimate as a hint for sorting, not an assessment: it never decides a tier.

### The Quadrant

After every file is scored, two thresholds split the grid:

- **Likely**: likelihood in the top 20% of the repo's non-test files. The cutoff is repo-relative (the likelihood of the file at the 20% rank, at least one file, ties included, never 0) because churn levels differ widely between repos. It is stored as `metadata.likely_cutoff`.
- **Impactful**: impact at or above `max(tiers.high, the impact at the top-20% rank of assessed non-test files)`, ties included. The percentile keeps the quadrant meaningful when the LLM scores generously and most files would clear a fixed bar; the `tiers.high` floor (61 by default) keeps small or low-stakes repos from calling modest impact impactful. The effective cutoff is stored as `metadata.impact_threshold` and shown next to every score ("How bad if it breaks?  92  HIGH  repo cutoff 78"). Scores are a relative signal within the repo, not a probability.

Every output a person reads names the axes as questions: "Will it break?" (likelihood) and "How bad if it breaks?" (impact), and names each quadrant by its posture, what to do about it.

The two questions are not equally backed, and `get`, the PR comment and the dashboard say so in one line ("Will it break? is validated on bug-fix history; How bad? is AI-assessed, not yet checked against maintainer ratings."). On a merged-PR calibration over 7 repos, "Will it break?" picked files that later got bug-fix PRs (top-10 precision 0.57 to 1.00, lift 2.2 to 4.4). "How bad if it breaks?" was positive but below plain PR activity in 5 of 7 repos; where repos use severity labels it reached a lift of about 2 to 2.8. It has not been checked against maintainer ratings yet.



| Quadrant | Posture (`quadrant_label`) | `quadrant` | Tier | What it asks for |
|---|---|---|---|---|
| Likely and impactful | Harden now | `fragile-critical` | CRITICAL | Senior owner sign-off, regression and contract tests, smaller PRs, auto-merge off |
| Impactful, stable | Protect | `protect` | HIGH | Owner review, block auto-merge, tests before merge when none sit next to the file |
| Likely, low impact | Stabilize | `watch` | MEDIUM | Regression tests when fixes recur, refactor what you touch; never blocks |
| Neither | Routine | `low` | LOW | Normal review; agents may auto-merge (autonomy L3) |

JSON keeps the `quadrant` values as stable machine IDs and adds `quadrant_label`; tier names stay as metadata and filters (`--tier`, `--fail-on`). The quadrant and its tier apply only to files whose impact was assessed. Other files keep the tier from their heat score, and `quadrant` is left out of the JSON.

### Heat Score

`heat_score` stays in the JSON for backward compatibility. With an assessed impact it is `max(likelihood, impact)`; otherwise it equals likelihood. Tiers of assessed files come from the quadrant, not from heat, so a file can have heat 90 and tier MEDIUM (likely, low impact).

### Test Files

Test files (`*_test.go`, `test_*.py`, `*_test.py`, `*.test.ts(x)`, `*.spec.js(x)` and the `.mjs`/`.mts` variants, `*Test.java`, `*Tests.kt`, `*Tests.cs`, `*_spec.rb`, `*_test.rb`) are capped just below the MEDIUM threshold on both axes and on heat, and they are never counted as likely. They import everything and change often, but a broken test does not reach users; ranked alongside source they pushed real hotspots down. `check calibration` leaves them out of the population for the same reason.

## Reasons and Actions

After quadrants are set, `analyze` attaches an `explanation` to every file: a verdict (the posture, or `Not assessed`), a headline, why its impact is high (or low), why it is likely (or not) to break, and what to do, split into before-merge and backlog actions. It is computed by rules in `internal/explain` from numbers already in the heatmap, with no extra LLM call, so the same heatmap always gives the same text.

Percentiles are repo-relative: "top 5%" means at most 5% of the repo's non-test files have a value at least as high (rounded up, so the repo maximum in 1,000 files is "top 1%"). A signal becomes a reason only when it is in the top 20% and above zero.

**Why impact.** With an LLM assessment: the impact dimensions at 60 or above, highest first (`security 92 · availability 85`), then the LLM's summary and critical reason. Without one: impact is an estimate from centrality and path keywords, and the reason says to run `analyze` with an LLM.

**Why likely.** Each reason quotes the number and its percentile:

| Signal | Reason when notable (top 20%) |
|---|---|
| Churn | commits in the last 90 days and authors overall (authors alone when churn is not notable) |
| Complexity | functions and cyclomatic complexity in the file, at the better of the two percentiles; line count instead when complexity is not notable |
| Importers | how many files import it |
| Recent fixes | fix commits among the last 10 changes (whole-word match on fix, bug, hotfix, patch, repair, correct, resolve), newest quoted with its date |
| Incidents | recorded incidents and the last date (any count) |
| No tests | no test file next to it by the language's convention (any file in a language with a convention) |

A test sits "next to" a file when one of these exists: `foo_test.go`; `test_foo.py`, `foo_test.py` or `tests/test_foo.py`; `foo.test.ts` or `foo.spec.ts` beside it or in `__tests__/` (also for `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`); `FooTest.java` or `FooTests.java` beside it or in the Maven `src/test/` mirror (also `.kt`, `.cs`); `foo_spec.rb` or `foo_test.rb`; for Rust, a `#[cfg(test)]` module in the file. Other languages have no convention, so `has_test_file` is left out and no reason is given. The test coverage score itself is unchanged.

**What to do.** Actions come from a rule table. Each has a `scope`: `before_merge` is what any PR touching the file needs, `backlog` is what to fix in the file over time. Each carries the `signal` that triggered it, and backlog items carry a copyable `command` (a `gh issue create` command, or for `codeowners` an anchored CODEOWNERS line naming the top two owners). Before-merge actions come first.

| Action | Scope | Postures | When |
|---|---|---|---|
| `senior-review` | before merge | Harden now | always; names the top two owners when known |
| `owner-review` | before merge | Protect | always; names the top two owners when known |
| `regression-tests` | before merge | Harden now, not assessed; Stabilize | at least one recent fix; Stabilize needs two or more |
| `no-tests` | before merge | Harden now, Protect, not assessed | no test next to the file and no regression-tests item already asks for tests |
| `stable-interface` | before merge | Harden now, Protect | importers in the top 10% ("Add contract tests") |
| `refactor` | before merge | Stabilize | complexity in the top 20% ("Refactor the function you touch") |
| `single-owner` | before merge | Stabilize, not assessed | top owner holds 80% or more of the file, it has 5 or more commits, and the repo has more than one author ("Add a second reviewer") |
| `no-auto-merge` | before merge | Harden now, Protect | always |
| `smaller-pr` | before merge | Harden now | `pr check` only: the diff is over 500 lines or touches 3+ components |
| `light-review` | before merge | Routine, and every test file | always (the only action; `pr check` leaves it out of the checklist) |
| `assess-impact` | backlog | not assessed | always (`highstakes analyze`) |
| `split` | backlog | Harden now, not assessed | functions or cyclomatic in the top 5% |
| `codeowners` | backlog | Harden now, Protect | no existing CODEOWNERS rule covers the file |
| `document-invariants` | backlog | Harden now | 5 or more authors with authors in the top 10%, or a single owner (as above) |
| `test-gap` | backlog | Protect | no test next to the file |
| `complexity-issue` | backlog | Stabilize | complexity in the top 20% |
| `ownership-issue` | backlog | Stabilize | a single owner (as above) |

A single owner never counts in a repo with one author overall (counted across every file's owners), so one-person repos get no `single-owner`, `ownership-issue` or owner-based `document-invariants` items: there is no one to name.

Whether a PR changes exported symbols is not detected, so no action depends on it. `pr check` marks before-merge items done when the PR already satisfies them, and only Harden now and Protect files' items go in its checklist; the rest are listed as worth doing, not blocking. See the [CLI reference](cli-reference.md#verdict-and-checklist).

**Agent autonomy.** After quadrants are set, `analyze` gives every file an autonomy level from its posture (Routine 3, Stabilize 2, Protect 1, Harden now 0; unassessed files use the posture of their tier), capped at 1 when impact is not LLM-assessed and at `autonomy.max_level`. See the [CLI reference](cli-reference.md#agent-autonomy).

Owners come from the same history pass as [Ownership](#ownership), run once during `analyze`. If git fails, actions stay generic and `analyze` prints a warning.

## Runtime Units

Imports say which code calls which; they do not say which code runs together. Two crates can share a library and still run in separate pods. Runtime units add that missing layer: `analyze` finds every deployable entry point and records what each one links.

Entry points, per language (test files and excluded directories are skipped):

| Language | Entry point | Name |
|----------|-------------|------|
| Rust | `[[bin]]` tables in `Cargo.toml`, else `src/main.rs`, `src/bin/*.rs` and `src/bin/*/main.rs` | the bin name |
| Go | a directory whose files declare `package main` and `func main()` | the directory (the module for a root `main`) |
| JS/TS | `package.json` `bin` (string or object), else the file a `start` script runs; `dist/x.js` maps to `src/x.ts` when only the source exists | the bin key or package name |
| Python | `[project.scripts]` or `[tool.poetry.scripts]` in `pyproject.toml`, and `__main__.py` | the script name or directory |

Each unit links every non-test file reachable from its entry file through the import graph. A Go unit starts from its whole `main` package (Go builds directories) and a Rust bin from its crate's `src/lib.rs` too (a bin links its own library). Files are grouped into components (crate, package, Python package, Go module, or the `cmd/`, `internal/`, `pkg/` package roots). A component linked by two or more units is **shared**, unless a unit owns it: a unit owns its home component when it is the only unit there or the one named after it, so a crate's main binary keeps its crate even when a helper bin or another binary links it.

Placement is inferred, best effort, and tagged with `file:line` evidence:

- a Dockerfile or Containerfile whose `ENTRYPOINT` or `CMD` runs a program named like the unit (shell wrappers such as `sh -c` are skipped);
- a Helm values file (any YAML under a directory with `Chart.yaml`) or Kubernetes manifest whose `image:` or `repository:` names the unit: the image name equals the unit name or the unit name ends with `-<image name>` (`acme/worker` matches `acme-worker`). Templated values (`{{ ... }}`) and files under `tests/` are skipped.

Units are stored in the heatmap `metadata.units`; every file gets `component` and `units` (the units that link it).

A unit is **supporting** (`supporting: true`) when its entry point sits in a supporting component (the same rule as the tour: `examples`, `scripts`, `tests`, `e2e`, `benchmarks`, ... and `tour.supporting` globs) or it is a helper bin: a Rust bin under `src/bin/` whose name or path has a tooling word (`perf`, `bench`, `gen`, `codegen`, `xtask`, `tool`, `dev`, `debug`, `test`, `fixture`, or their plurals, as a whole word between `/`, `-`, `_` or `.`). Other `src/bin/` targets stay product units, since they can be real runtime entry points (an init process inside a VM). `list units`, `get architecture` and the dashboard unit view hide supporting units unless asked.

**Declared links.** What imports cannot show (a gRPC session between two binaries, a driver the gateway calls over a socket, which pod runs what) a human declares in `.heatmap/architecture.yaml`, each claim with `file:line` evidence (see [Configuration](configuration.md#architecture-declarations)). Every output keeps declared links and placements apart from inferred ones: dashed and marked "declared".

**Generated pictures.** `get architecture --format seecode` turns units, imports and declarations into a SeeCode architecture spec: the top product units by max impact (ties: more Harden now files), the shared library the most units import directly, and one "other units" box for the rest, within SeeCode's budget (9 boxes, 12 arrows by default). Arrows are declared links first, then imports by importing-file count. Rows follow dependency levels (callers above what they use); placement groups are the declared placement, else the first inferred hint. Every box lists its top 3 files by impact as evidence, with the posture and the first action. `get tour --format walkthrough` turns the tour into data for the show-me step-through: one box per component, one step per stop. Neither is hand-drawn, so a regenerated picture follows the code.

**Limits.** Units come from entry points and imports, not from runtime truth. A unit lists what a binary can execute, not what a deployment actually starts: feature flags, `cfg` gates, dynamic loading, plugins, RPC between processes and configuration-selected drivers are invisible, and an import the resolver misses drops the code it leads to. Placement says where a binary is packaged, not where or how many copies run; a Helm chart that picks images at install time, or a Dockerfile that runs a script, yields no hint. Examples and helper binaries are units too when they declare an entry point; they are flagged supporting by name, which can still hide a production helper whose name sounds like tooling, or show a tool whose name does not: check with `--include-supporting`. Treat units as a map to check against the deployment, not as the deployment.

## Static Analysis by Language

Go is parsed with `go/ast`. Rust, TypeScript, JavaScript, Java, Kotlin, C#, C, C++ and Ruby are parsed with [gotreesitter](https://github.com/odvcencio/gotreesitter), a pure-Go tree-sitter runtime, so `go install` needs no C toolchain. Each file is parsed once, in parallel. Python still uses line heuristics, as do files over 1 MB or files the parser rejects.

Complexity is counted from decision nodes, the same way for every language:

- **Cyclomatic**: 1 per function or method, plus 1 per `if`/`else if`, loop, `case`/match arm, `catch`/`rescue`, ternary, and each `&&`/`||`.
- **Cognitive**: 1 plus the current nesting depth per `if`, loop, `switch`/`match`/`when`, `catch` and ternary; `else if` costs 1 without nesting; lambdas and blocks raise the nesting depth; nesting resets at each function. Go (go/ast) follows the same rules, so the same code scores the same in every language.

Both feed the same complexity score (cyclomatic capped at 20, cognitive at 30).

Imports are resolved to files in the repo. Anything that does not name a repo file (standard library, third-party packages) is dropped; nothing is fuzzy-matched.

| Language | Resolved |
|---|---|
| Go | Same-module imports via `go.mod`; a package import credits every non-test file in that directory |
| Rust | `mod x;` (`x.rs` or `x/mod.rs`), `use crate::`, `use self::`, `use super::` (each `src/bin/<name>.rs` or `src/bin/<name>/main.rs` binary is its own crate root), and `use other_crate::` for crates in the same repo (by `Cargo.toml` name) |
| TypeScript / JavaScript | Relative specifiers with extension and `index` resolution (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`), TypeScript's `./x.js` to `x.ts` (then `x.tsx`) convention, tried before the exact `x.js` when the importer is TypeScript, `require()` and dynamic `import()`, and `tsconfig.json`/`jsconfig.json` `paths` and `baseUrl` (nearest config, relative `extends` followed) |
| Java / Kotlin | `import a.b.C` to the Kotlin file that declares top-level `C` (class, interface, object, `fun` or `val`) in `package a.b`, whatever the file is named, else the file `C.java`/`C.kt` declaring `package a.b`; wildcard imports credit the whole package; static imports resolve to their class |
| C# | `using A.B` credits every file declaring namespace `A.B`; `using static A.B.T` and `using X = A.B.T` go to the file declaring type `T` in namespace `A.B` (block or file-scoped), whatever the file is named |
| C / C++ | `#include` paths relative to the file, the repo root, `include/` and `src/` |
| Ruby | `require_relative`, and `require` under `lib/` |

Known gaps: Java and Kotlin classes in the same package reference each other without imports, so those edges are missing; npm workspace packages resolve only when `tsconfig` `paths` map them.

## Git History

Change frequency, unique authors and recent changes come from a single `git log --name-only` pass over the whole repository, so every file gets its history regardless of repo size. If the log cannot be read (no `git` binary, a repository without commits), `analyze` still runs, prints a warning on stderr and records the reason in the heatmap's `metadata.git_history` field (`"complete"` or `"unavailable: <reason>"`); change frequency is then 0 for every file.

## Tier Assignment

Files with an LLM-assessed impact take the tier of their quadrant (see above). Files without one take the tier of their heat score:

| Tier | Score Range | Review Requirements |
|------|-------------|---------------------|
| CRITICAL | 86-100 | 2 senior reviewers, security scan, no auto-merge |
| HIGH | 61-85 | 2 reviewers, integration tests, no auto-merge |
| MEDIUM | 31-60 | 1 reviewer |
| LOW | 0-30 | Auto-merge safe |

## Caching

LLM assessments are cached in `.heatmap/cache/` by a SHA-256 hash of the model ID, the prompt version and the file content. If a file hasn't changed since last analysis and the model is the same, its cached assessment is reused. This makes re-analysis near-free. Switching `--model` re-assesses files instead of reusing another model's results.

Cache files are JSON containing the four impact scores, summary, and critical reason. Delete `.heatmap/cache/` or run `highstakes analyze --force` to re-assess everything.

## File Filtering

Infra and config files are assessed alongside source code: CI workflows under `.github/`, Helm charts and Kubernetes manifests (`.yaml`, `.yml`), `.toml`, `.json`, Dockerfiles and Terraform (`.tf`). A broken deploy manifest or CI workflow can have as much blast radius as code.

Not all files are sent to the LLM. Skipped:

- Test files (`*_test.go`, `test_*.py`, `*.spec.ts`, etc.)
- Generated files (`*.pb.go`, `*.generated.*`, `zz_generated*`)
- Docs (`.md`)
- Lock files (`go.sum`, `go.mod`, `*.lock`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`)
- Excluded directories (configured in `.heatmap/config.yaml`)

### What is sent, and what is never sent

Only with an API key set, and only to the endpoint you configure (OpenRouter by default, or `HIGHSTAKES_API_URL`), HighStakes sends each assessed file's repo-relative path and its first 300 lines. Nothing else about the repo leaves your machine: no git history and no other file contents.

In a git repo, `analyze` only looks at files git tracks or would track (`git ls-files --cached --others --exclude-standard`). `.gitignore` excludes untracked files only: a file that is committed is analysed, and may be sent, even if it matches an ignore pattern. For tracked files the protection is the sensitive-file denylist below. Outside a git repo it walks the directory tree.

Files that look like credentials are never sent, even when they are tracked. They still appear in the heatmap with static data and `"llm_skipped": "sensitive"`, and a file caught by name is never read into a prompt. A file is sensitive when:

- Its name matches `credentials.json`, `auth.json`, `credentials.enc`, `mcp-oauth-tokens.json`, `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `id_dsa*`, `id_ecdsa*`, `id_ed25519*`, `service-account*.json`, `*secret*.json`, `*secret*.yaml`, `*secret*.yml`, `kubeconfig`, `kubeconfig.*`, `.npmrc`, `.pypirc`, `.netrc`, `terraform.tfstate*` or `*.tfvars` (case-insensitive), or it sits under `.aws/` or `.ssh/`, or it is `.docker/config.json`.
- Its first 300 lines contain a private key header (`-----BEGIN ... PRIVATE KEY-----`), a `"private_key":` field, an `aws_secret_access_key` assignment, or a GitHub (`ghp_`), OpenAI-style (`sk-`) or Slack (`xoxb-`, `xoxp-`) token.

## PR Risk: Importance and Change Risk

`pr check` keeps two questions apart. Importance is the max impact of the files a PR touches (heat for files without an LLM assessment): how bad a mistake there would be. Change risk scores the diff itself: how likely this particular change is to contain one (lines changed, new files, config or infra files, lines deleted from hot files, test ratio). The PR tier, and any `--fail-on` gate, is the most severe tier among the touched files; change risk tells a reviewer how much attention the diff needs. See the [CLI reference](cli-reference.md#two-axis-risk) for the formula.

## Ownership

Owners are derived from one `git log --numstat` pass. Each author's score for a file is the lines they changed in it, each commit weighted by `0.5^(age_days / 180)`, so recent work counts most. Scores are normalized to shares per file. `.mailmap` is applied, bots are skipped, and the `owners` config map turns emails into GitHub handles. `pr check` ranks reviewers by share times file heat; `create codeowners` ranks owners of a directory by their summed share across its hot files.

## Is It Right?

`highstakes check calibration` tests the scores against git history: did the files the heatmap calls hot attract reverts, bug fixes, security fixes and incidents? It reports per-tier event rates, precision@K, lift over random and rank correlation, for the heat score, for the two axes (`likelihood`, `impact`) and for each factor on its own. See the [CLI reference](cli-reference.md#highstakes-check-calibration).

Read the results with two limits in mind. Fix history measures how often code breaks, while blast radius measures how bad it is when it does; a stable payment module can deserve HIGH (protect) with no fixes at all, because CRITICAL needs both high impact and high likelihood, so expect blast radius and impact to correlate weakly with fix counts. Judge likelihood by fix prediction and impact by whether its ranking matches what your team would protect. And churn is computed from the same history, so use `--holdout` (score with history before a cutoff, count events after it) before trusting any churn number.

### Evidence from pull requests

Commit-message keywords are noisy ("fix typo" counts as a fix). `highstakes check calibration --source prs` uses merged GitHub pull requests instead, with file lists from local git:

- **Will it break?** Files touched by bug-fix PRs (labelled bug, fix, defect or regression on the PR or a closed issue, or titled `fix:`) and revert PRs. With `--holdout`, likelihood is scored with history before the cutoff and these PRs are counted after it. The commit-keyword result for the same window is shown next to it.
- **How bad if it breaks?** Signs that maintainers treated a change to a file as serious, none of which come from the LLM: the PR was backported to a release branch, reverted within 7 days, carried a severity label (security, critical, p0, p1, sev, incident, CVE), had 2+ approvers or 3+ reviews, or the file is covered by CODEOWNERS. Blast radius is compared with each signal and with their count per file.

Review depth and CODEOWNERS track maintainer judgment, which is what impact should match, but also code volume and activity; read a positive correlation with that in mind, next to the `pr_activity` reference row (blast radius against the count of all PRs touching each file). See the [CLI reference](cli-reference.md#pull-request-source) for the exact rules.

### Validating impact

Fix history validates likelihood (how often a file breaks). Blast radius is the impact axis (how bad a break would be), and needs a different check. There are three:

- **Security fixes only.** `highstakes check calibration --events security --since 1095d` counts only security fixes, which tend to land where a bug hurts most. A positive blast-radius Spearman and precision@K above the base rate here is evidence for the impact axis. Security fixes are rare: the report warns when fewer than 10 files have one, and most repos need a window of years.
- **Pull-request stakes.** `highstakes check calibration --source prs --since 365d` compares blast radius with backports, fast reverts, severity labels, review depth and CODEOWNERS coverage (see [Evidence from pull requests](#evidence-from-pull-requests)). It needs no maintainer time, but the signals are indirect.
- **Maintainer labels.** `highstakes create labels --out labels.csv` writes a blind sheet: the 20 files HighStakes rates highest on blast radius mixed with 20 random other files, shuffled, with no scores shown. Maintainers rate each from 1 to 5 (5 = "a bug here would be a serious incident"). `highstakes check calibration --labels labels.csv` then reports Spearman and precision@K (positive = rating 4 or 5) for blast radius and heat. This is the direct test of impact, and the one to trust when security fixes are too few.
