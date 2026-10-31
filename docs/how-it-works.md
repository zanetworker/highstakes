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
        ├── Combine factors with weights (LLM 40%, static 30%, git 20%, manual 10%)
        ├── Assign tier (CRITICAL/HIGH/MEDIUM/LOW)
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

| Factor | Weight | Source |
|--------|--------|--------|
| Change Frequency | 40 | Commits in last 90 days (if the file has history) |
| Complexity | 30 | Cyclomatic + cognitive complexity |
| Incident History | 25 | Manual incident records (if any) |
| Dependency Centrality | 10 | How many files import this one (normalized 0-1) |
| Path Keywords | 5 | The larger of user impact and data sensitivity, both from path keywords |

Weights are relative and normalize over the factors present. They were chosen on the [calibration benchmark](../bench/README.md): across seven repos in five languages, churn and complexity predicted which files needed fixes later (holdout), centrality added a little, and test coverage is a placeholder today, so it carries no weight without the LLM. Path keywords ("auth", "token", "payment", "user" and similar in the file path) are a weak signal: they match on names alone and cannot tell a core auth module from an SDK helper. They count once, as the larger of user impact and data sensitivity, so a file whose only signal is its name stays in the LOW tier.

### Test Files

Test files (`*_test.go`, `*.test.ts`, `*.spec.js`, `test_*.py` and similar) are capped just below the MEDIUM threshold in both modes. They import everything and change often, but a broken test does not reach users; ranked alongside source they pushed real hotspots down. `check calibration` leaves them out of the population for the same reason.

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

`pr check` keeps two questions apart. Importance is the max heat of the files a PR touches: how bad a mistake there would be. Change risk scores the diff itself: how likely this particular change is to contain one (lines changed, new files, config or infra files, lines deleted from hot files, test ratio). The tier and any `--fail-on` gate follow importance; change risk tells a reviewer how much attention the diff needs. See the [CLI reference](cli-reference.md#two-axis-risk) for the formula.

## Ownership

Owners are derived from one `git log --numstat` pass. Each author's score for a file is the lines they changed in it, each commit weighted by `0.5^(age_days / 180)`, so recent work counts most. Scores are normalized to shares per file. `.mailmap` is applied, bots are skipped, and the `owners` config map turns emails into GitHub handles. `pr check` ranks reviewers by share times file heat; `create codeowners` ranks owners of a directory by their summed share across its hot files.

## Is It Right?

`highstakes check calibration` tests the scores against git history: did the files the heatmap calls hot attract reverts, bug fixes, security fixes and incidents? It reports per-tier event rates, precision@K, lift over random and rank correlation, for the heat score and for each factor on its own. See the [CLI reference](cli-reference.md#highstakes-check-calibration).

Read the results with two limits in mind. Fix history measures how often code breaks, while blast radius measures how bad it is when it does; a stable payment module can deserve CRITICAL with no fixes at all, so expect blast radius to correlate weakly with fix counts. And churn is computed from the same history, so use `--holdout` (score with history before a cutoff, count events after it) before trusting any churn number.
