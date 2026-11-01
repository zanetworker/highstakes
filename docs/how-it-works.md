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
        └── Calculate review requirements per tier
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

The [calibration benchmark](../bench/README.md) showed why they stay apart. Churn and complexity predicted which files needed fixes later: with the weights below, the ten top-ranked files on the primary repos were all fixed after the holdout cutoff (precision@10 1.00, Spearman 0.43). Blast radius was consistent across four models (rank agreement 0.72 to 0.90) but correlated weakly with fixes (Spearman 0.18 in-sample and 0.23 holdout with the default model), because a stable auth module can be critical without ever breaking. Blending blast radius into one score at 40% made fix prediction worse for every model (holdout Spearman 0.37 with the default model). So each axis keeps its meaning, and the tier comes from where a file sits on both.

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
- **Impactful**: impact at or above the HIGH threshold (61 by default, `tiers.high` in the config), stored as `metadata.impact_threshold`.

| Quadrant | `quadrant` | Tier | Review guidance |
|---|---|---|---|
| Likely and impactful | `fragile-critical` | CRITICAL | Likely to break and costly when it does: senior review, add tests first |
| Impactful, stable | `protect` | HIGH | Stable but costly if broken: keep owners on review, never auto-merge |
| Likely, low impact | `watch` | MEDIUM | Changes and breaks often, limited damage: refactor or add tests |
| Neither | `low` | LOW | Light review |

The quadrant and its tier apply only to files whose impact was assessed. Other files keep the tier from their heat score, and `quadrant` is left out of the JSON.

### Heat Score

`heat_score` stays in the JSON for backward compatibility. With an assessed impact it is `max(likelihood, impact)`; otherwise it equals likelihood. Tiers of assessed files come from the quadrant, not from heat, so a file can have heat 90 and tier MEDIUM (likely, low impact).

### Test Files

Test files (`*_test.go`, `test_*.py`, `*_test.py`, `*.test.ts(x)`, `*.spec.js(x)` and the `.mjs`/`.mts` variants, `*Test.java`, `*Tests.kt`, `*Tests.cs`, `*_spec.rb`, `*_test.rb`) are capped just below the MEDIUM threshold on both axes and on heat, and they are never counted as likely. They import everything and change often, but a broken test does not reach users; ranked alongside source they pushed real hotspots down. `check calibration` leaves them out of the population for the same reason.

## Reasons and Actions

After quadrants are set, `analyze` attaches an `explanation` to every file: a headline, why its impact is high (or low), why it is likely (or not) to break, and what to do. It is computed by rules in `internal/explain` from numbers already in the heatmap, with no extra LLM call, so the same heatmap always gives the same text.

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

**What to do.** Actions come from a rule table, most important first. `pr check` shows the first three.

| Action | Quadrants | When |
|---|---|---|
| `assess-impact` | not assessed | always |
| `senior-review` | fragile-critical | always; names the top two owners when known |
| `owner-review` | protect | always; names the top two owners when known |
| `codeowners` | fragile-critical, protect | always |
| `change-carefully` | protect | always |
| `ai-review` | watch | always |
| `light-review` | low, and every test file | always (the only action) |
| `split` | fragile-critical, not assessed | functions or cyclomatic in the top 5% |
| `refactor` | watch | complexity in the top 20% |
| `regression-tests` | fragile-critical, protect, not assessed | at least one recent fix |
| `add-tests` | watch | two or more recent fixes |
| `stable-interface` | fragile-critical, protect | importers in the top 10% |
| `no-tests` | all but low | no test found and no fix-driven test action already asks for tests |
| `single-owner` | all but low | top owner holds 80% or more of the file and it has 5 or more commits |
| `knowledge-spread` | all but low | 5 or more authors and authors in the top 10% |

Owners come from the same history pass as [Ownership](#ownership), run once during `analyze`. If git fails, actions stay generic and `analyze` prints a warning.

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
| Rust | `mod x;` (`x.rs` or `x/mod.rs`), `use crate::`, `use self::`, `use super::`, and `use other_crate::` for crates in the same repo (by `Cargo.toml` name) |
| TypeScript / JavaScript | Relative specifiers with extension and `index` resolution (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.mts`, `.cts`), TypeScript's `./x.js` to `x.ts` convention, `require()` and dynamic `import()`, and `tsconfig.json`/`jsconfig.json` `paths` and `baseUrl` (nearest config, relative `extends` followed) |
| Java / Kotlin | `import a.b.C` to the file `C.java`/`C.kt` declaring `package a.b`; wildcard imports credit the whole package; static imports resolve to their class |
| C# | `using A.B` credits every file declaring namespace `A.B` |
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

## PR Risk: Importance and Change Risk

`pr check` keeps two questions apart. Importance is the max impact of the files a PR touches (heat for files without an LLM assessment): how bad a mistake there would be. Change risk scores the diff itself: how likely this particular change is to contain one (lines changed, new files, config or infra files, lines deleted from hot files, test ratio). The PR tier, and any `--fail-on` gate, is the most severe tier among the touched files; change risk tells a reviewer how much attention the diff needs. See the [CLI reference](cli-reference.md#two-axis-risk) for the formula.

## Ownership

Owners are derived from one `git log --numstat` pass. Each author's score for a file is the lines they changed in it, each commit weighted by `0.5^(age_days / 180)`, so recent work counts most. Scores are normalized to shares per file. `.mailmap` is applied, bots are skipped, and the `owners` config map turns emails into GitHub handles. `pr check` ranks reviewers by share times file heat; `create codeowners` ranks owners of a directory by their summed share across its hot files.

## Is It Right?

`highstakes check calibration` tests the scores against git history: did the files the heatmap calls hot attract reverts, bug fixes, security fixes and incidents? It reports per-tier event rates, precision@K, lift over random and rank correlation, for the heat score, for the two axes (`likelihood`, `impact`) and for each factor on its own. See the [CLI reference](cli-reference.md#highstakes-check-calibration).

Read the results with two limits in mind. Fix history measures how often code breaks, while blast radius measures how bad it is when it does; a stable payment module can deserve CRITICAL with no fixes at all, so expect blast radius and impact to correlate weakly with fix counts. Judge likelihood by fix prediction and impact by whether its ranking matches what your team would protect. And churn is computed from the same history, so use `--holdout` (score with history before a cutoff, count events after it) before trusting any churn number.

### Validating impact

Fix history validates likelihood (how often a file breaks). Blast radius is the impact axis (how bad a break would be), and needs a different check. There are two:

- **Security fixes only.** `highstakes check calibration --events security --since 1095d` counts only security fixes, which tend to land where a bug hurts most. A positive blast-radius Spearman and precision@K above the base rate here is evidence for the impact axis. Security fixes are rare: the report warns when fewer than 10 files have one, and most repos need a window of years.
- **Maintainer labels.** `highstakes create labels --out labels.csv` writes a blind sheet: the 20 files HighStakes rates highest on blast radius mixed with 20 random other files, shuffled, with no scores shown. Maintainers rate each from 1 to 5 (5 = "a bug here would be a serious incident"). `highstakes check calibration --labels labels.csv` then reports Spearman and precision@K (positive = rating 4 or 5) for blast radius and heat. This is the direct test of impact, and the one to trust when security fixes are too few.
