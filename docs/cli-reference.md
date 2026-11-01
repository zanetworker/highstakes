# CLI Reference

All commands support `--json` for machine-readable output.

## highstakes

Launch the interactive TUI explorer (run with no subcommand).

```sh
highstakes
```

Keyboard: `↑↓` navigate, `Enter` expand, `f` filter by tier, `s` sort, `/` search, `q` quit.

## highstakes init

Initialize heatmap in the current repository.

```sh
highstakes init [--json]
```

Creates `.heatmap/` directory and `.heatmap/config.yaml` with default settings.

## highstakes analyze

Analyze the repository and generate the heatmap.

```sh
highstakes analyze [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--no-llm` | `false` | Skip LLM analysis, use static heuristics only |
| `--force` | `false` | Re-assess all files, ignoring cache |
| `--model` | `openai/gpt-6-luna` | OpenRouter model identifier |
| `--concurrency` | `10` | Number of parallel LLM requests |
| `--json` | `false` | Output summary as JSON |

Analysis continues without git history (no `git` binary, a repository without commits): `analyze` prints a warning on stderr, change frequency is 0 for every file, and the heatmap records it in `metadata.git_history` (`"complete"`, or `"unavailable: <reason>"`).

## highstakes get

Get a file's verdict, both questions with their repo cutoffs, what to do before the next merge, what to put on the backlog, and why.

```sh
highstakes get <file> [--json]
```

Text output, top to bottom: the path; the verdict (`HARDEN NOW`, `PROTECT`, `STABILIZE`, `ROUTINE`, or `NOT ASSESSED` without an LLM); "Will it break?" and "How bad if it breaks?", each with its score, `HIGH` or `LOW`, and the repo cutoff (an unassessed impact shows as `~N estimate`); one line saying scores are a relative signal within the repo, not a probability, and one saying how far each question is backed ("Will it break? is validated on bug-fix history; How bad? is AI-assessed, not yet checked against maintainer ratings."); **Do before the next merge** and **Put on the backlog** checklists, each item with the signal that triggered it (`why:`) and, for backlog items, a copyable command; **Why** (break signals and break cost, quoting the file's numbers and repo percentiles); and the latest fix commit. `--json` returns the file record, including `quadrant_label` and `explanation` (see [File fields](#file-fields)), plus `likely_cutoff` and `impact_threshold`. Heatmaps written before explanations existed fall back to the per-axis lines and blast radius reasoning; run `analyze` again to get the sections. See [How It Works](how-it-works.md#reasons-and-actions) for the rules.

If the file is not found, suggests similar paths from the heatmap.

**Exit code 4** if file not found.

## highstakes get tour

Get a reading path for learning the repository: the most important product code first, each section after the sections it depends on. Makes no LLM calls; reads `.heatmap/heatmap.json`, `.heatmap/config.yaml` (for `tour.supporting`) and the source files (for line anchors).

```sh
highstakes get tour [--per-component N] [--min-impact N] [--components a,b] [--include-supporting] [--limit N] [--focus <dir|file>] [--format text|json|walkthrough] [--out FILE] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--per-component` | `0` | Keep the N most important files of every component, one section per component (0 = off). Takes precedence over `--limit` |
| `--min-impact` | `0` | Skip files whose impact is below this (0-100); components left without files drop out. Applies before `--per-component` picks, so every stop shown reaches it |
| `--components` | all | Only these components, comma-separated, named as in the section titles (e.g. `crates/core,internal/api`). A supporting component named here is shown without `--include-supporting` |
| `--include-supporting` | `false` | Add supporting components (see below) in a final part titled "Supporting: CI, deployment, scripts and examples" |
| `--limit` | `0` | Keep the N most impactful files across the repo (0 = all). Selection happens before ordering. Ignored with `--per-component` |
| `--focus` | `""` | Only files under this repo-relative directory, or this one file. Matches whole path segments (`api` does not match `apiv2`) |
| `--format` | `text` | `text`, `json`, or `walkthrough` (data for the show-me rich step-through; see below). Another value exits 2 listing these |
| `--out` | stdout | Write the output to this file instead |
| `--json` | `false` | Same as `--format json` |

Ordering:

1. Files are grouped into sections by directory (one Go package per section).
2. Sections are ranked by importance: max impact, then the number of fragile-critical stops, then max heat, then name.
3. Sections are emitted in that rank, except that a section's dependencies come first: before emitting a section, every selected section it imports (directly, or through sections that were not selected) is emitted, most important first. So a high-impact package that imports a low-impact one is read right after it. Sections in one import cycle have no order constraint between them and keep importance order. The import graph is the whole repo's, so the order holds even when `--limit` drops the sections in between.
4. Product sections come first, then supporting sections (with `--include-supporting`), each part ordered by the same rule. A product section that imports a supporting one still comes first.
5. Within a section, files go by impact, then dependency centrality. Heat breaks remaining ties, so heatmaps from before the two axes still order sensibly.

With `--per-component`, sections are components instead of directories, the graph is the component import graph, and files within a component go fragile-critical first, then by impact, likelihood and centrality, among the files that pass `--min-impact`. A repo-wide `--limit` can fill every stop from two or three hot components; `--per-component` covers every part of the repo, which suits onboarding.

`level` is still reported (dependency depth: 0 imports no other section, cycles share a level), but it no longer decides the order.

Supporting components hold code around the product: any component with a path segment named `examples`, `scripts`, `.github`, `docs`, `deploy`, `tests`, `e2e`, `benchmarks`, `fixtures`, `vendor` or `third_party`, plus `tour.supporting` globs in `.heatmap/config.yaml` (see [configuration](configuration.md#tour-supporting-components)). They are hidden by default, in both modes, unless `--include-supporting` is given, they are named in `--components`, or `--focus` points inside one. The text header counts the product components shown and the supporting components hidden.

A component is the top-level unit a file belongs to, decided in this order:

1. The shallowest directory below the repo root that holds `Cargo.toml` (Rust crate), `package.json` (JS/TS package), `pyproject.toml` or `__init__.py` (Python package) or `go.mod` (Go module). Units nested inside it fold into it.
2. Under `cmd/`, `internal/` and `pkg/`, the top two path segments (`internal/tour`).
3. Otherwise the top-level directory, and `.` for files at the repo root.

Every section carries its `component` in the JSON output, in both modes.

Each stop shows its verdict, both questions with the repo cutoffs ("Will it break? 50 HIGH, cutoff 35 · How bad if it breaks? 92 HIGH, cutoff 78"), its `why` (the impact reason: LLM `critical_reason`, else `summary`), the first break signal, and the top before-merge and top backlog action.

Test files (`*_test.go`, `test_*.py`, `*_test.py`, `*.test.ts(x)`, `*.spec.js(x)` and the `.mjs`/`.mts` variants, `*Test.java`, `*Tests.kt`, `*Tests.cs`, `*_spec.rb`, `*_test.rb`, `tests.rs`, `*_tests.rs`, `*_test.rs`, and any file under a `tests/` directory) are left out of the tour and of the dependency order: they import everything and say little about where the stakes are.

`tour` is a subcommand of `get`, so `highstakes get tour` runs the tour and `highstakes get <file>` still looks up a file. A file literally named `tour` at the repo root cannot be looked up with `get`; use `list` instead.

Line anchors point at the first top-level declaration (`func`, `type`, `class`, `def`, `export`, `fn`, `pub`, `struct`, `enum`, `trait`, `impl`, `mod` and similar).

**Exit code 2** for a negative `--limit` or `--per-component`, a `--min-impact` outside 0-100, an unknown component in `--components` (the error lists the valid names), or a malformed `tour.supporting` glob. **Exit code 4** if `--focus` matches no file, no file reaches `--min-impact`, or only hidden supporting components are left (the error suggests `--include-supporting`).

JSON schema (version `1`), shaped for `/show-me walkthrough` and coding agents: sections are the grouped narrative, stops carry `file:line` anchors.

```json
{
  "version": "1",
  "repo": "/path/to/repo",
  "commit": "abc1234",
  "branch": "main",
  "analyzed_at": "2026-10-30T12:37:55+01:00",
  "focus": "internal",
  "ordering": "sections by importance (max impact, then fragile-critical count), each after the sections it depends on; product before supporting; stops by impact, then centrality",
  "total_stops": 8,
  "product_components": 6,
  "supporting_components": 0,
  "supporting_hidden": 2,
  "sections": [
    {
      "order": 1,
      "title": "internal/types",
      "dir": "internal/types",
      "component": "internal/types",
      "kind": "product",
      "level": 0,
      "max_heat": 31,
      "max_impact": 45,
      "tier": "medium",
      "depends_on": [],
      "used_by": ["internal/scorer", "pkg/heatmap"],
      "stops": [
        {
          "order": 1,
          "path": "internal/types/types.go",
          "line": 6,
          "anchor": "internal/types/types.go:6",
          "title": "types.go",
          "heat": 31,
          "tier": "medium",
          "impact": 45,
          "likelihood": 31,
          "impact_assessed": true,
          "quadrant": "low",
          "why": "imported by 20 files",
          "summary": "LLM blast radius summary (when assessed)",
          "critical_reason": "LLM critical reason (when assessed)",
          "top_impact": "security",
          "depends_on": [],
          "depends_on_count": 0,
          "used_by": ["internal/scorer/scorer.go"],
          "used_by_count": 20,
          "owners": ["@owner", "alice"],
          "notes": "annotation notes (when set)"
        }
      ]
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `product_components`, `supporting_components` | Components shown, by kind |
| `supporting_hidden` | Supporting components left out (pass `--include-supporting`) |
| `sections[].kind` | `product` or `supporting` |
| `sections[].level` | Dependency depth of the section; 0 = imports no other section. Informational; ordering is by importance with dependencies first |
| `sections[].depends_on` / `used_by` | Directories this one imports / that import it |
| `stops[].order` | Global 1-based reading position |
| `stops[].line`, `anchor` | First top-level declaration (`func`, `type`, `class`, `def`, `export`, `pub`, ...), else 1 |
| `sections[].max_impact`, `tier` | Highest impact and most severe tier among the section's stops |
| `stops[].impact`, `likelihood`, `impact_assessed`, `quadrant` | The two axes; see [File fields](#file-fields) |
| `stops[].why` | `critical_reason`, else `summary`, else static signals (importers, recent commits, incidents) |
| `stops[].top_impact` | Highest LLM impact dimension: `security`, `data`, `availability` or `user`; omitted when not assessed |
| `stops[].depends_on` / `used_by` | Up to 5 files, hottest first; `*_count` is the full number |
| `stops[].owners` | Annotation owner first, then recent commit authors by commit count; up to 3 |
| `stops[].explanation` | The file's explanation (headline, verdict, impact and likelihood reasons, actions); see `get`. The text tour prints the first likelihood reason as `break signals:` |
| `stops[].verdict`, `top_before_merge`, `top_backlog` | The file's posture and the first action of each scope (each `{id, text, scope, signal, command}`); the text tour prints them as `before merge:` and `backlog:` |
| `likely_cutoff`, `impact_threshold` | Top-level: the repo cutoffs the stop scores read against |

### Walkthrough format

`--format walkthrough` writes the data file of the show-me rich step-through (the `/show-me walkthrough` skill's `walkthrough_build.py` turns it into one self-contained HTML page):

```sh
highstakes get tour --per-component 1 --min-impact 85 --format walkthrough --out tour-walkthrough.data.json
python3 <show-me skill>/scripts/walkthrough_build.py tour-walkthrough.data.json   # writes tour-walkthrough.html
```

| Walkthrough element | From the tour |
|---------------------|---------------|
| Box (node) | One per component, in tour order, left to right and top to bottom; name without the `<repo>-` prefix; sub line = max impact of its stops and its role (owning unit, or "N units" for a shared library, drawn darker) |
| Flow | One per part: `product`, and `supporting` with `--include-supporting`; plus "How components connect" (`wiring`) |
| Link (wiring step) | One per import link between drawn boxes, from the importing component to its dependency, payload = number of importing non-test files; the strongest 30 links |
| Step (hop) | One per stop, travelling from the previous stop's box (a self step when it stays in one component) |
| Payload chip | The stop's `file:line` anchor |
| Rail gist | `do:` the first action, else `read <file>` |
| Caption | The anchor and verdict, both questions with the repo cutoffs, why it matters (critical reason or summary), the first break signal, and the top before-merge and backlog action. The hop's short label is the top before-merge action |
| Color | The stop's posture (Harden now, Protect, Stabilize, Routine; grey for an impact estimate) |
| Key | HighStakes' own legend: the posture colors in use, reading order, imports, shared library. The template's network channel labels (browser, server to server, refused) are hidden through `legendExtra` |

All heatmap text is HTML-escaped. The map holds 28 boxes; further components are left out and counted in the intro (narrow with `--components`, `--focus` or `--min-impact`). **Exit code 4** when the tour has no stops.

## highstakes get architecture

Get a generated architecture diagram of the runtime units, ready for SeeCode. Reads `.heatmap/heatmap.json`, `.heatmap/architecture.yaml` (declared links and placements; see [Configuration](configuration.md#architecture-declarations)) and the source files (evidence line anchors). No LLM calls.

```sh
highstakes get architecture [--format seecode|json] [--max-nodes 9] [--out FILE] [--include-supporting] [--json]
node <seecode skill>/scripts/seecode.mjs render arch.json    # after --out arch.json: writes arch.html
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `seecode` | `seecode` (a SeeCode `architecture` spec) or `json` (the same diagram as plain data). Another value exits 2 listing these |
| `--max-nodes` | `9` | Most boxes (2-24). The arrow budget follows SeeCode: 12 up to 9 boxes, 20 up to 16, else 36 |
| `--out` | stdout | Write to this file instead |
| `--include-supporting` | `false` | Include supporting units (examples, scripts, tests, helper bins) |
| `--json` | `false` | Same as `--format json` |

What it draws:

- **Boxes**: the product units by max impact (ties: more fragile-critical files, then name). When they fit, all of them plus the shared library the most units import directly. Otherwise the top `max-nodes - 2`, that library (from 4 boxes on; else `max-nodes - 1` units), and one muted "N other units" box for the rest. The highest-impact unit is `focal`.
- **Arrows**: declared links first (dashed, SeeCode kind `async`, legend "declared (architecture.yaml)"), then imports between boxes labelled with the number of importing files, most first. Arrows over the budget are dropped and counted in the caption. Links into folded units point at "other units".
- **Groups**: a unit's declared placement (dashed, "(declared)"), else its first inferred deployment hint ("(inferred)": Dockerfile name, `helm <values key>`, `k8s <manifest>`).
- **Layout**: top to bottom; rows by dependency level over all arrows, drawn or not (callers above what they use), each row centered and ordered to reduce crossings.
- **Evidence**: every box lists its top 3 files by impact as `{id, file, line, note}`, `note` = posture (or impact estimate) and the first action. SeeCode shows them in the box's detail panel.

Text is clipped to SeeCode's limits (box label 40, sub 48, arrow label 28, group label 32 characters).

JSON (`--format json`): `title`, `repo`, `commit`, `nodes[]` (`id`, `label`, `sub`, `tag`, `kind` = `unit`, `library` or `other`, `members`, `max_impact`, `impact_assessed`, `fragile_critical`, `focal`, `group`, `row`, `col`, `files[]` = `{path, line, impact, quadrant, note}`), `edges[]` (`from`, `to`, `label`, `source` = `imports` or `declared`, `count`, `both`, `evidence`), `groups[]` (`id`, `label`, `source` = `declared` or `inferred`, `evidence`), `product_units`, `supporting_hidden`, `dropped_edges`.

**Exit code 2** for a bad flag or an invalid `architecture.yaml` (unknown unit names are listed with the valid ones). **Exit code 4** with no heatmap, no units, or only supporting units (add `--include-supporting`).

## highstakes list

List files sorted by heat, likelihood or impact. Text rows show heat, `L` (likelihood) and `I` (impact; `I~` marks an estimate without the LLM).

```sh
highstakes list [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tier` | (all) | Filter by tier: `critical`, `high`, `medium`, `low` |
| `--limit` | `0` | Maximum files to return (0 = all) |
| `--sort` | `heat` | Sort by `heat`, `likelihood` or `impact` (descending, ties by path) |
| `--json` | `false` | Output as JSON |

JSON rows keep `path`, `heat_score`, `tier` and `reason`, and add `likelihood` ("Will it break?"), `impact` ("How bad if it breaks?"), `impact_assessed` and `quadrant`.

**Exit code 2** if `--tier` or `--sort` is invalid (error includes valid values).

### File fields

Every file in `.heatmap/heatmap.json` (and `get <file> --json`) carries both axes. See [How It Works](how-it-works.md#two-axes-likelihood-and-impact) for the formulas.

| Field | Meaning |
|-------|---------|
| `likelihood` | 0-100, chance the file needs fixes (churn, complexity, centrality, path keywords, incidents) |
| `impact` | 0-100, damage if it breaks: LLM blast radius, or an estimate from centrality and path keywords when not assessed |
| `impact_assessed` | `true` when impact came from the LLM |
| `quadrant` | `fragile-critical`, `protect`, `watch` or `low` (stable machine IDs, unchanged); omitted when impact is not assessed |
| `quadrant_label` | The posture people read: `Harden now`, `Protect`, `Stabilize` or `Routine` (same order as `quadrant`) |
| `heat_score` | `max(likelihood, impact)` when assessed, else likelihood (kept for compatibility) |
| `tier` | From the quadrant when assessed, else from `heat_score` |
| `explanation` | `headline`, `verdict` (posture, or `Not assessed`), `impact_high`, `likely`, `impact_reasons`, `likelihood_reasons` and `actions`: each `{id, text, scope, signal, done, command, owners}`, `scope` = `before_merge` (this PR) or `backlog` (the file over time), before-merge first. `done` is always false in the heatmap (`pr check` sets it); `command` is a copyable `gh issue create` command or CODEOWNERS line on backlog items; `owners` names suggested owners on review items |
| `factors.test_coverage.has_test_file` | `true` or `false` when a test sits next to the file by its language's convention; omitted for languages without one |
| `component` | The crate, package or module the file belongs to |
| `units` | Runtime units that link the file (see `list units`); omitted when none does |

The heatmap `metadata` adds `likely_cutoff` (lowest likelihood counted as likely: top 20% of non-test files) and `impact_threshold` (lowest impact counted as impactful: the top 20% of assessed non-test files, never below `tiers.high`, 61 by default). Heatmaps written before these fields existed load with zeros; run `analyze` again to fill them.

## highstakes list units

List runtime units: the deployable entry points `analyze` found (Rust bins, Go `main` packages, `package.json` bin and start scripts, Python scripts and `__main__.py`) and what each links through the import graph. Sorted by max impact, then name.

```sh
highstakes list units [--include-supporting] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--include-supporting` | `false` | Show supporting units too: entry points in a supporting component (`examples`, `scripts`, `tests`, `e2e`, `benchmarks`, ..., plus `tour.supporting` globs) and helper bins (Rust `src/bin/` targets named like tooling: `perf`, `bench`, `gen`, `codegen`, `xtask`, `tool`, `dev`, `debug`, `test`, `fixture`). Hidden ones are counted on the last line |
| `--json` | `false` | Output as JSON |

Text output per unit: name, kind, entry file and the `file:line` that declares it; max impact (`~` when the file supplying it was not LLM-assessed), Harden now file count and linked file count; `own` components (linked by this unit only), `links units` (another unit's crate linked in-process), `shared` components (linked by two or more units), each declared link (`declared link: -> | <- | <-> <unit>: <label> [evidence]`) and declared placement from `.heatmap/architecture.yaml`, and each inferred placement with its evidence (`none found` when there is none).

JSON: `{"units": [...], "count": N, "supporting_hidden": N, "declared": {"links": [...], "placement": [...]}}`. Each unit has `name`, `kind` (`rust-bin`, `go-main`, `node-bin`, `node-start`, `python-script`, `python-main`), `entry`, `evidence`, `home` (the entry's component), `components` (home first), `shared`, `files`, `lines`, `max_impact`, `impact_assessed`, `fragile_critical`, `supporting` (omitted when false) and `inferred_placement` (each `{kind, detail, evidence}`, kind `dockerfile`, `helm` or `k8s`). The same list is stored in the heatmap `metadata.units`.

A repo with no entry points (a library) prints a note and exits 0; so does a heatmap written before units existed (run `analyze` again). **Exit code 4** if there is no heatmap; **exit code 2** if `.heatmap/architecture.yaml` is invalid. See [How It Works](how-it-works.md#runtime-units) for detection rules and limits.

## highstakes pr check

Assess risk of the current diff against a base branch.

```sh
highstakes pr check [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--base` | `main` | Base branch to compare against |
| `--json` | `false` | Output as JSON (same as `--format json`) |
| `--format` | `text` | `text`, `markdown` (the PR comment the triage workflow posts) or `json`; anything else exits 2 |
| `--approvals` | (none) | Comma-separated GitHub logins that approved the PR; a suggested owner among them ticks `senior-review` and `owner-review` items. Only CI knows approvals: the shipped workflow fills it from the GitHub API |
| `--fail-on` | (none) | Exit 5 when the PR tier is at or above this tier: `critical`, `high`, `medium`, `low` (`critical` blocks Harden now files) |
| `--shadow` | `false` | Compute the `--fail-on` decision (default gate `critical`), append it to `.heatmap/shadow.jsonl`, always exit 0 |
| `--author` | HEAD commit author | Email or handle excluded from suggested reviewers |
| `--reviewers` | `3` | Max suggested reviewers; `0` skips the git history lookup |

Uses `git diff --no-renames --numstat` to identify changed files (a rename is its old path deleted plus its new path added), then looks up their scores. The PR tier is the most severe tier among the touched files (so a PROTECT file with a modest heat score still makes the PR HIGH); `heat_score` is the highest heat among them.

A changed file that is not in the heatmap (for example, a file added in this PR) does not default to LOW. It inherits the highest heat score and the most severe tier of scored files in its directory, or the nearest parent directory that has any: files with an LLM-assessed impact contribute their quadrant tier (so a new file next to a Harden now file is CRITICAL even at heat 75), other files are tiered from heat with the repo's `tiers` from `.heatmap/config.yaml`. It is marked `"unscored": true` in `--json` output, and adds an `Unscored new file(s)` circuit breaker signal. Docs (`.md`, `.mdx`, `.rst`, `.txt`) are exempt and stay LOW, except dependency manifests and lock files (`requirements*.txt`, `go.mod`, `package.json`, `Cargo.toml`, `Cargo.lock`, `pyproject.toml`, `Gemfile`, `pom.xml`, `build.gradle`, ...), which inherit heat and count as config/infra changes in change risk.

### Verdict and checklist

The verdict is the posture of the most severe changed file: `Harden now`, `Protect`, `Stabilize` or `Routine` (it follows the PR tier, so `--fail-on critical` blocks exactly the Harden now verdict). Text output leads with it, then **Do before merge**: the before-merge actions of every changed file, deduplicated (identical items list every file they apply to and are done only when done for all), then each file with both scores, the backlog and the gate decision. Routine files add nothing to the checklist.

`pr check` ticks items the PR already satisfies:

| Item | Done when |
|------|-----------|
| `regression-tests`, `no-tests`, `stable-interface` | the PR adds or changes a test next to the file (deleting one does not count; a renamed test counts at its new path): a conventional test path (`foo_test.go`, `test_foo.py`, `foo.test.ts`, `FooTest.java`, ...), a test under the file's module directory (Rust `src/proxy/tests/x.rs` for `src/proxy.rs`), `<stem>_*` test files beside it, or a `tests/` sibling naming the file. Tests added inside a Rust file's own `#[cfg(test)]` module are not detected |
| `codeowners` | the CODEOWNERS file at HEAD (`.github/CODEOWNERS`, `CODEOWNERS` or `docs/CODEOWNERS`, the first found) covers the path, last matching rule with owners wins |
| `senior-review`, `owner-review` | a login in `--approvals` matches one of the item's owners (case and `@` ignored) |

Harden now files get one change-dependent item, `smaller-pr` ("Ask for a smaller PR"), when the diff changes more than 500 lines or touches non-test files in 3 or more components. Whether exported symbols changed is not detected.

`--format markdown` renders the PR comment: a `<!-- highstakes-triage -->` marker, the verdict heading (`⛔ Harden before merge`, `🛡 Protect`, `🔧 Stabilize`, `✅ Routine`), one sentence naming the riskiest file(s), the checklist (`[x]` done, `[ ]` open), a table of at most five non-routine files (File, Will it break?, How bad?, Action), then one `N more files need action` row for any further non-routine files and one Routine row for the routine ones, and a folded **Why these actions?** block (covering every non-routine file) with the same two notes (relative signal; evidence for each question), per-file reasons, repo cutoffs, suggested reviewers, backlog items with their commands and the gate decision.

JSON adds `verdict`, `checklist` and `backlog` (actions with `id`, `text`, `scope`, `signal`, `done`, `files`, and `command` for backlog items), `likely_cutoff` and `impact_threshold`; each `files_changed[]` entry adds `likelihood`, `impact`, `impact_assessed`, `quadrant`, `quadrant_label`, and `actions` (its before-merge items with `done`).

An empty diff prints `No changes detected` (JSON: `{"changes": 0, "tier": "low", "verdict": "Routine"}`) and still goes through the gate: `--fail-on low` blocks it, and `--shadow` logs its decision. When a gate applies, the JSON adds `decision`, `fail_on` and `shadow`.

**Exit code 2** for an invalid `--fail-on` or `--format` (error lists valid values). **Exit code 3** if git diff fails (error includes valid base examples). **Exit code 5** when `--fail-on` blocks the PR.

### Two-axis risk

The tier (the most severe tier among touched files) and importance answer "how important is the code this PR touches". A second, independent number answers "how risky is this change itself". Text output shows the verdict and change risk with its factors; `--json` has all of these:

| JSON field | Meaning |
|------------|---------|
| `importance` | 0-100, max impact of touched files; files without an LLM assessment count with their heat |
| `change_risk` | 0-100, sum of: lines changed (up to 30), new files (5 each, up to 15), config or infra files such as CI workflows, Dockerfiles, Helm, Terraform, dependency manifests and lock files (20), lines deleted from high or critical files (up to 20), low test ratio (up to 15) |
| `change_risk_level` | `low` (<30), `medium` (30-59), `high` (60+) |
| `change_risk_factors` | Components worth 5 or more points, in plain words |

A one-line fix to a critical file is high importance, low change risk. A 2,000-line refactor of utilities is the reverse.

### Suggested reviewers

`suggested_reviewers` lists up to `--reviewers` people who own the hottest changed files (top 5, docs skipped), ranked by ownership share times file heat. Ownership comes from `git log --numstat`: lines each author changed in the file, weighted by recency (half-life 180 days), with `.mailmap` applied and bots ignored. The PR author is excluded. Each entry has `handle`, `email`, `score` and `files`. Handles come from the `owners` map in `.heatmap/config.yaml`, or from GitHub noreply addresses; otherwise the email is used. In CI, check out with full history (`fetch-depth: 0`) or owners will be incomplete.

### Gate and shadow mode

`--fail-on high` makes `pr check` exit 5 when the PR tier is high or critical, and adds `fail_on` and `decision` (`pass` or `block`) to the JSON. `--shadow` computes the same decision, defaulting the gate to `critical`, but never fails: it prints the decision to stderr, sets `"shadow": true`, and appends one JSON line per run to `.heatmap/shadow.jsonl` (time, base, head, tier, heat, change risk, decision). Run in shadow mode until the decisions match what your reviewers would have done, then switch to `--fail-on`.

## highstakes create codeowners

Generate a CODEOWNERS block for high-stakes paths, with suggested owners from git history.

```sh
highstakes create codeowners [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--min-tier` | `high` | Lowest tier to include: `critical`, `high`, `medium`, `low` |
| `--max-owners` | `2` | Owners per path |
| `--write` | `false` | Write the block into `--file` instead of only printing it |
| `--file` | `.github/CODEOWNERS` | File updated by `--write` |
| `--json` | `false` | Output as JSON |

A file is hot when its tier is at or above `--min-tier`, or when its LLM-assessed impact is at or above that tier's threshold (61 for `high`, 86 for `critical`), so stable but impactful code is protected even if it rarely changes. A directory with at least two scored files, all hot, becomes one `/dir/` rule; otherwise each hot file gets its own rule. Owners are ranked the same way as `pr check` reviewers. Paths with no owner in git history are written as comments, so they never clear an earlier rule. GitHub's CODEOWNERS has no documented way to escape characters, so a path containing whitespace or one of `\`, `*`, `?`, `[`, `]` cannot be a rule that matches only that path. Such paths are written as `# cannot express in CODEOWNERS (space in path): <path>` (or `(glob character in path)`) comments, reported on stderr and listed in JSON `unrepresentable`; protect them by hand, for example by owning their directory.

The block sits between `# BEGIN highstakes` and `# END highstakes` markers. `--write` replaces that block in place, or appends it to the end of the file, where CODEOWNERS rules take precedence. Lines outside the markers are left alone.

JSON fields: `min_tier`, `entries` (each with `path`, `is_dir`, `tier`, `heat_score`, `files`, `owners`), `unrepresentable` (hot paths left as comments), `block` (the text), `written` (path, empty unless `--write`).

**Exit code 2** for an invalid `--min-tier` (error lists valid values). **Exit code 3** if git history cannot be read. **Exit code 4** if there is no heatmap.

## highstakes create policy

Generate a machine-readable list of paths that AI reviewers must not auto-approve.

```sh
highstakes create policy [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--min-tier` | `high` | Lowest tier to include: `critical`, `high`, `medium`, `low` |
| `--json` | `false` | Output JSON instead of YAML |

Files are selected the same way as `create codeowners`: tier at or above `--min-tier`, or assessed impact at or above that tier's threshold. Prints YAML by default. Fields: `version`, `generated_by`, `min_tier`, `description`, `paths` (globs: `dir/**` for fully hot directories, exact paths otherwise) and `files` (every hot file with `path`, `tier`, `heat_score` and the LLM `reason` when available). Feed `paths` to the path restrictions of Copilot code review, CodeRabbit path instructions, or your own approval bot.

```sh
highstakes create policy > .github/highstakes-policy.yaml
```

**Exit code 2** for an invalid `--min-tier`. **Exit code 4** if there is no heatmap.

## highstakes create labels

Write a blind CSV sheet for maintainers to rate how bad a bug in each file would be. Score the filled sheet with [`check calibration --labels`](#maintainer-labels).

```sh
highstakes create labels [--top 20] [--random 20] [--seed 1] [--out FILE] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--top` | `20` | Files HighStakes rates highest on blast radius (on heat when no file has an LLM assessment) |
| `--random` | `20` | Other non-test files, sampled at random |
| `--seed` | `1` | Random seed; the same seed and heatmap give the same sheet |
| `--out` | (stdout) | Write the CSV to this file |
| `--json` | `false` | Print `ranked_by`, `top`, `random`, `seed`, `files`, `out` and `paths` (sheet order) as JSON |

The sheet has the columns `path`, `maintainer_rating` (blank) and `notes`. Maintainers rate each file from 1 to 5, where 5 means "a bug here would be a serious incident", and leave a row blank when unsure. The two groups are shuffled together and the sheet carries no HighStakes scores, so the raters cannot anchor on them. Test files are never sampled.

```sh
highstakes create labels --out labels.csv
# maintainers fill in maintainer_rating, then:
highstakes check calibration --labels labels.csv
```

**Exit code 2** for a negative `--top` or `--random`, or both 0. **Exit code 4** if there is no heatmap or it has no non-test files.

## highstakes incident create

Record a production incident for a file.

```sh
highstakes incident create --file <path> --severity <level> --description <text> [--date YYYY-MM-DD] [--json]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--file` | Yes | File path |
| `--severity` | Yes | `critical`, `high`, `medium`, or `low` |
| `--description` | Yes | What happened |
| `--date` | No | Incident date (default: today) |

Incidents are stored in `.heatmap/incidents.json` and feed into the heat score on next `highstakes analyze`.

## highstakes incident list

List recorded incidents.

```sh
highstakes incident list [--file <path>] [--json]
```

## highstakes report

Generate a heat distribution report.

```sh
highstakes report [--limit N] [--json]
```

Text output shows files grouped by tier with blast radius reasoning. JSON output returns the full heatmap data.

## highstakes dashboard

Generate an interactive HTML dashboard and open it in the browser.

```sh
highstakes dashboard [--output <path>] [--no-open]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `.heatmap/dashboard.html` | Output file path |
| `--no-open` | `false` | Don't open in browser |

The dashboard opens on the action inbox: before-merge cards (one per Harden now or Protect file: verdict, path, every before-merge step with its signal, and owners; the top 10, plus files tied with the tenth, with **Show all** for the rest) and backlog cards (one per kind of fix across files, ordered by how many Harden now files it covers; open one to see its files and a **Copy** button for one snippet with every CODEOWNERS line or `gh issue create` command; when the clipboard is blocked the snippet is selected for copying by hand). Files without an LLM-assessed impact (`analyze --no-llm`, or no API key) take the posture of their estimated impact against the recorded cutoffs (`likely_cutoff`, `impact_threshold`), and their cards and backlog entries are marked "impact estimated (run with an LLM for an assessment)", with the same note in the inbox intro; LLM-assessed files rank before estimated ones. Heatmaps without recorded cutoffs show only assessed files. Behind tabs (arrow keys, Home and End move between them) are four investigation views: a treemap (visual overview), a file explorer (hierarchical tree), an architecture view and a quadrant view. All support the posture filter (one per tier), search, and a detail panel with the verdict, both questions with their repo cutoffs and the evidence note, the before-merge and backlog checklists, and the reasons and impact dimensions folded under **Why**. The page is static: it copies commands and never calls GitHub.

The architecture view draws one node per directory (Go package) in dependency layers, entry points on top. Node size is total lines; color is the tier of the highest LLM-assessed impact in the directory (the most severe heat tier when nothing there was assessed), and the number is that impact. A dashed outline and an "N likely" badge mark directories with files likely to need fixes. The label names the hottest file. Test files are left out of nodes and edges. Edges come from the import graph: an arrow from A to B with label N means N files in A import a file in B (same-directory imports are not drawn). Clicking a node lists its files hottest-first in the detail panel; clicking a file there opens its blast radius. Clicking an arrow lists the file-level imports behind it (importer -> imported, with each file's impact; at most 200 per arrow). The layout is computed in the page (layered, with barycenter ordering to reduce crossings; layers wider than about 1500px wrap into extra rows), so the HTML stays self-contained. The `-`, `+`, **Fit** and **1:1** buttons, the mouse wheel and trackpad or touch pinch zoom; drag to pan. The view fits the screen when first opened.

**Group by: Unit** redraws the view by runtime unit (see `list units`): a top row of unit boxes, each holding the components only that unit links, and a band below with the shared libraries, layered by their own imports. Unit boxes are coloured by the max impact of their own files, carry the same fragility marker, and show the inferred placement (Dockerfile name, Helm values key, Kubernetes manifest). Clicking a unit shows its entry point and evidence, placement evidence, totals across everything it links, own components, shared libraries and files with impact, likelihood and quadrant; clicking a shared library shows the units that link it. The hint line counts non-test files no unit links. The toggle is disabled when the heatmap has no units. Supporting units are hidden until **Supporting units** is on (the hint line counts them; the button is disabled when there are none). Links declared in `.heatmap/architecture.yaml` draw as dashed accent arrows labelled "(declared)", with the evidence in the tooltip, and a declared placement replaces the inferred one on the box as "declared: <runs_in>". An invalid `architecture.yaml` makes `dashboard` exit 2.

The quadrant view plots every non-test file with "Will it break?" on x and "How bad if it breaks?" on y. A vertical line marks the likely cutoff and a horizontal line the impact threshold; each quadrant is labelled with its posture and what it asks for. Faded dots have an estimated impact. Click a dot to open the file in the detail panel.

## highstakes github install

Generate a GitHub Actions workflow for automated PR triage.

```sh
highstakes github install
```

Creates `.github/workflows/heatmap-triage.yml`, the same workflow this repo runs (installing highstakes with `go install ...@latest`): it posts `pr check --format markdown` as a PR comment, fills `--approvals` from the PR's reviews, and fails the `triage` job on Harden now files (`--fail-on critical`). See [CI Integration](ci-integration.md#github-actions).

## highstakes agent-context

Output machine-readable JSON describing all commands, flags, exit codes, and available models.

```sh
highstakes agent-context
```

Designed for AI agents to discover the CLI surface programmatically. The output includes a `schema_version` field for detecting breaking changes.

## highstakes check calibration

Compare heat scores with what actually happened in git history.

```sh
highstakes check calibration [--since 365d | --holdout <cutoff>] [--events all] [--top 10] [--limit 10] [--json]
highstakes check calibration --source prs [--repo owner/name] [--since 365d | --holdout <cutoff>] [--top 10] [--limit 10] [--json]
highstakes check calibration --labels FILE [--top 10] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `365d` | Count events in the last N days (format `Nd`) |
| `--holdout` | (off) | Cutoff as `YYYY-MM-DD` or `Nd` ago. Churn and incident history are recomputed from history up to the cutoff, files that did not exist yet are dropped, and events are counted after it. Cannot be combined with `--since` |
| `--events` | `all` | Event kinds to count: `all` (reverts, bug fixes, security fixes, incidents), `fixes` (bug-fix and security commits), `reverts`, or `security` (security fixes only). Incidents count only with `all` |
| `--source` | `commits` | Where events come from: `commits` (commit-message keywords, below) or `prs` (merged GitHub pull requests, see [Pull-request source](#pull-request-source)) |
| `--repo` | (origin remote) | GitHub `owner/name` for `--source prs`; by default read from the `origin` remote URL |
| `--labels` | (off) | Compare with maintainer ratings in this CSV instead of git history. Cannot be combined with `--since`, `--holdout`, `--events` or `--source` |
| `--top` | `10` | K for precision@K and recall@K |
| `--limit` | `10` | Maximum files per surprise list |
| `--json` | `false` | Output as JSON |

**Events.** Each non-merge commit in the window is labelled from its message: a revert (subject starts with `Revert` or the body has `This reverts commit`), a bug fix (fix, bug, bugfix, patch, hotfix, repair, correct or resolve, with their plural and past forms, as whole words in the subject, so "debug", "prefix", "dispatch" and "incorrect" do not count), or a security fix (`CVE-` or `GHSA-` anywhere in the message, or `security` as a standalone word or a word starting with `vuln` in the subject). The security keywords do not count in `docs`, `ci`, `test`, `chore`, `build` or `style` commits, nor inside names such as `Content-Security-Policy`, `podSecurityContext`, `SecurityException` or `SECURITY.md`. Every file the commit touched gets one event. Commits touching more than 50 files are skipped as bulk changes. Incidents in `.heatmap/incidents.json` dated in the window add one event to their file.

**Metrics.**

| Metric | Definition |
|--------|------------|
| Base rate | Share of scored files with at least one event |
| Hit rate (per tier) | Share of files in the tier with at least one event; `events/file` is the mean event count |
| precision@K | Share of the K highest-scoring files with at least one event. Files tied at the K-th score share the remaining slots, so ties give the average over every ordering |
| recall@K | Share of all files with events that are in the top K |
| Lift | precision@K divided by the base rate; 1.0 means no better than a random pick |
| Spearman | Rank correlation between the score and the event count, with average ranks for ties; `n/a` when a side has no variation |

Metrics are reported for `heat`, for the two axes (`likelihood`; `impact`, on LLM-assessed files only when any are assessed, else the estimate) and for `blast_radius` (LLM-assessed files only), `churn`, `complexity` and `centrality` alone. In holdout mode likelihood is recomputed at the cutoff and quadrant tiers are reassigned from it. Surprises list LOW-tier files with the most events and CRITICAL/HIGH files with none.

The report starts with the event kinds counted (`event_filter` in JSON). When fewer than 10 scored files have events it prints a `WARNING` line and sets `too_few_events`: the numbers are then anecdotes, not evidence.

**Security-only mode.** Fix history measures how likely a file is to break; blast radius scores how bad a break would be, so fix counts cannot validate it. `--events security` counts only security fixes, a rough proxy for impact: read the `blast_radius` row first. Security fixes are rare, so widen the window (`--since 1095d`) and expect too few events on young repos.

**Leakage.** In the default (in-sample) mode, churn and incident history come from the same history as the events, so they look better than they are. `--holdout` removes that for churn and incidents. Complexity, centrality, path heuristics and blast radius still come from today's file contents, so some leakage remains.

### Pull-request source

`--source prs` takes events from merged pull requests instead of commit messages, and adds a second comparison for the impact axis. It needs the `gh` CLI, logged in.

1. **PR metadata from GitHub.** Merged PRs in the window (merged after `--since`'s start or the `--holdout` cutoff), via `gh api graphql`, 50 per page: number, title, merge time, merge commit, author, labels, the issues it closes and their labels, review count, distinct approvers and commit count. Each PR is cached in `.heatmap/cache/github/<owner>/<name>/pr-<n>.json`; after a complete run only PRs updated since then are fetched again. A cache written before a field was added (such as the commit count) is refetched once. A run reads at most 1,500 PRs and stops while 200 GraphQL points remain or when GitHub reports a rate limit; it then reports what it has (`fetch.capped`, `fetch.partial` in JSON) and adds a caveat. A partial run is not recorded as complete, so the next run fetches again.
2. **Files from local git.** Each PR is mapped to the commit that landed it on HEAD's first-parent history: the merge commit GitHub reports, else a subject ending in `(#123)` (squash merge) or starting with `Merge pull request #123`. Files changed come from that commit (merge commits against their first parent). For a rebase merge (a one-parent merge commit whose subject does not name the PR, from a PR with several commits), GitHub reports only the last rebased commit, so the files are the union over that commit and its first-parent ancestors, walking back only while an ancestor's subject matches one of the PR's commit headlines (the last 100 are fetched) and at most up to the PR's commit count. A rebase merge without the PR number in its subject still maps through the merge SHA GitHub reports. Unmapped PRs (PRs merged after HEAD, or whose landed commit is not on HEAD's first-parent history) are left out; the report shows how many mapped.
3. **Event rules per PR.**

| Evidence | Axis | Rule |
|---|---|---|
| Bug fix | fragility | A label matching `bug`, `fix`, `defect` or `regression` (case-insensitive) on the PR or an issue it closes, or a title `fix:` / `fix(scope):` |
| Revert | fragility | Title starts with `Revert` |
| Backported | stakes | A commit on a remote release line (branch name after the remote starting with `release`, `backport`, `stable` or `v<digit>`, or ending in `<digit>.x` such as `tokio-1.43.x`), made on the line itself (on its first-parent history but not HEAD's) and dated in the window, says `(cherry picked from commit <sha>)` for the PR's commit, or has a `(#N)` in its subject that is the PR's number or appears in the subject of the PR's commit (fixes landed on the release line first and merged forward as `fix (#N) (#M)`) |
| Fast revert | stakes | A commit on HEAD says `This reverts commit <sha>` for the PR's commit, or `Reverts [owner/repo]#N`, within 7 days of the merge |
| Severity label | stakes | A label on the PR or an issue it closes matching `security`, `critical`, `p0`, `p1`, `sev`, `incident`, `cve` or `vulnerab`, unless it also says `low`, `minor` or `trivial` |
| Review depth | stakes | 2 or more distinct approvers, or 3 or more reviews |
| CODEOWNERS | stakes | The file is owned in `.github/CODEOWNERS`, `CODEOWNERS` or `docs/CODEOWNERS` (last matching rule wins; a rule with no owners un-owns). A file-level signal, no PR needed |

PRs touching more than 50 files are skipped, as with commits.

4. **Metrics.** The main report (same layout and JSON fields as the commit source, with `source: "prs"`) ranks files against bug-fix and revert PRs: the "Will it break?" check, normally read on the `likelihood` row with `--holdout`. The same window with commit-keyword events follows side by side (`commit_keyword` in JSON). Then blast radius, on LLM-assessed non-test files, is compared with each stakes signal (PR counts per file) with `combined`, the number of distinct stakes signals per file, and with `pr_activity`, a reference row counting every mapped PR (not a stakes signal and not in `combined`; a signal that does no better than it mostly measures activity): Spearman, precision@K, lift, files with events, and `too_few_events` under 10 files. A signal is `n/a` when its data is missing: no release branches in the clone (fetch them), no CODEOWNERS file, or no LLM assessment.

JSON adds `github` (`repo`, `fetch` with `fetched`, `from_cache`, `api_calls`, `capped`, `cap`, `partial`, `partial_reason`; `in_window`, `mapped`, label counts, `backport_branches`, `codeowners_file`), `commit_keyword` (a full commit-source report) and `stakes` (each `name`, `available`, `note`, `files`, `files_with_events`, `too_few_events`, `base_rate`, `precision_at_k`, `recall_at_k`, `lift`, `spearman`).

Caveats: labels follow each project's habits (some never label PRs, so bug fixes rest on `fix:` titles). Review depth and CODEOWNERS reflect how important maintainers judge a file, which is what blast radius should match, but they also rise with code volume and with how often a file changes, so a positive correlation partly measures activity. Compare each with the `pr_activity` row.

### Maintainer labels

`--labels FILE` reads a sheet made by [`create labels`](#highstakes-create-labels) and filled in by maintainers. Rows with a blank rating are skipped; columns are found by header name, so extra columns and any order are fine. For `blast_radius` (LLM-assessed files only) and `heat` it reports, over the rated files in the heatmap:

| Metric | Definition |
|--------|------------|
| Spearman | Rank correlation between the score and the 1-5 rating |
| precision@K | Share of the K highest-scoring rated files with a rating of 4 or 5 |
| Lift | precision@K divided by the share of rated files with 4 or 5 |

It also prints how many files were rated, how many are in the heatmap, and the rated paths it could not find. Under 10 rated files in the heatmap it prints a `WARNING` (`too_few` in JSON). JSON fields: `rated`, `matched`, `unmatched`, `positives`, `base_rate`, `k`, `too_few`, `signals` (each `name`, `available`, `note`, `files`, `precision_at_k`, `lift`, `spearman`).

**Exit code 2** for invalid flags (bad `--since`/`--holdout` format, a future cutoff, `--top` below 1, both `--since` and `--holdout`, an unknown `--events` or `--source` value (the error lists the valid ones), `--events` with `--source prs`, `--repo` without `--source prs` or not `owner/name`, no `--repo` and no GitHub `origin` remote, `--labels` with a history flag) or a malformed labels CSV (the error names the line: a rating outside 1-5, a duplicate path, a missing `path` or `maintainer_rating` column). **Exit code 3** if git history cannot be read, or, with `--source prs`, the GitHub request fails for a reason other than rate limiting (rate limits give partial results instead). **Exit code 4** if no heatmap or labels file exists.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Internal error |
| `2` | Invalid input (bad flags, missing required params) |
| `3` | External dependency failure (git, API) |
| `4` | Not found (file not in heatmap) |
| `5` | PR blocked by `pr check --fail-on` |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENROUTER_API_KEY` | OpenRouter API key (default provider) |
| `HIGHSTAKES_API_KEY` | API key for any OpenAI-compatible endpoint (overrides `OPENROUTER_API_KEY`) |
| `HIGHSTAKES_API_URL` | Base URL for the API endpoint (used with `HIGHSTAKES_API_KEY`) |

Set either `OPENROUTER_API_KEY` or `HIGHSTAKES_API_KEY` + `HIGHSTAKES_API_URL`. Neither is needed with `--no-llm`.
