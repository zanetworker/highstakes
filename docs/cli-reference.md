# CLI Reference

All commands support `--json` for machine-readable output.

## heatmap

Launch the interactive TUI explorer.

```sh
heatmap
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

## heatmap get

Get why a file is impactful, why it is likely to break, and what to do about it.

```sh
highstakes get <file> [--json]
```

Text output is a headline with the quadrant, both axes and the repo's likely cutoff, then three sections: why impact is high (or low), why it is likely to break (or the likelihood signals when it is below the cutoff), and what to do. Reasons quote the file's numbers and their repo percentile. A review requirements line follows. `--json` returns the file record, including `explanation` (see [File fields](#file-fields)). Heatmaps written before explanations existed fall back to the per-axis lines and blast radius reasoning; run `analyze` again to get the sections. See [How It Works](how-it-works.md#reasons-and-actions) for the rules.

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
| `--components` | all | Only these components, comma-separated, named as in the section titles (e.g. `crates/openshell-core,internal/tour`). A supporting component named here is shown without `--include-supporting` |
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

Each stop shows impact, likelihood and quadrant, and its `why` leads with the impact reason (LLM `critical_reason`, else `summary`).

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
| `stops[].explanation` | The file's explanation (headline, impact and likelihood reasons, actions); see `get`. The text tour prints the first likelihood reason as `likely because:` and the top two actions as `do:` |

### Walkthrough format

`--format walkthrough` writes the data file of the show-me rich step-through (the `/show-me walkthrough` skill's `walkthrough_build.py` turns it into one self-contained HTML page):

```sh
highstakes get tour --per-component 1 --min-impact 85 --format walkthrough --out openshell-walkthrough.data.json
python3 <show-me skill>/scripts/walkthrough_build.py openshell-walkthrough.data.json   # writes openshell-walkthrough.html
```

| Walkthrough element | From the tour |
|---------------------|---------------|
| Box (node) | One per component, in tour order, left to right and top to bottom; name without the `<repo>-` prefix; sub line = max impact of its stops and its role (owning unit, or "N units" for a shared library, drawn darker) |
| Flow | One per part: `product`, and `supporting` with `--include-supporting` |
| Step (hop) | One per stop, travelling from the previous stop's box (a self step when it stays in one component) |
| Payload chip | The stop's `file:line` anchor |
| Rail gist | `do:` the first action, else `read <file>` |
| Caption | The anchor, both axes and quadrant, why it matters (critical reason or summary) and the first likelihood reason |
| Color | The stop's quadrant (fragile-critical, protect, watch, low; grey for an impact estimate) |

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
- **Evidence**: every box lists its top 3 files by impact as `{id, file, line, note}`, `note` = quadrant (or impact estimate) and the first action. SeeCode shows them in the box's detail panel.

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

JSON rows keep `path`, `heat_score`, `tier` and `reason`, and add `likelihood`, `impact`, `impact_assessed` and `quadrant`.

**Exit code 2** if `--tier` or `--sort` is invalid (error includes valid values).

### File fields

Every file in `.heatmap/heatmap.json` (and `get <file> --json`) carries both axes. See [How It Works](how-it-works.md#two-axes-likelihood-and-impact) for the formulas.

| Field | Meaning |
|-------|---------|
| `likelihood` | 0-100, chance the file needs fixes (churn, complexity, centrality, path keywords, incidents) |
| `impact` | 0-100, damage if it breaks: LLM blast radius, or an estimate from centrality and path keywords when not assessed |
| `impact_assessed` | `true` when impact came from the LLM |
| `quadrant` | `fragile-critical`, `protect`, `watch` or `low`; omitted when impact is not assessed |
| `heat_score` | `max(likelihood, impact)` when assessed, else likelihood (kept for compatibility) |
| `tier` | From the quadrant when assessed, else from `heat_score` |
| `explanation` | `headline`, `impact_high`, `likely`, `impact_reasons`, `likelihood_reasons` and `actions` (each `{id, text}`, most important first) |
| `factors.test_coverage.has_test_file` | `true` or `false` when a test sits next to the file by its language's convention; omitted for languages without one |
| `component` | The crate, package or module the file belongs to |
| `units` | Runtime units that link the file (see `list units`); omitted when none does |

The heatmap `metadata` adds `likely_cutoff` (lowest likelihood counted as likely: top 20% of non-test files) and `impact_threshold` (impact counted as impactful: `tiers.high`, 61 by default). Heatmaps written before these fields existed load with zeros; run `analyze` again to fill them.

## highstakes list units

List runtime units: the deployable entry points `analyze` found (Rust bins, Go `main` packages, `package.json` bin and start scripts, Python scripts and `__main__.py`) and what each links through the import graph. Sorted by max impact, then name.

```sh
highstakes list units [--include-supporting] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--include-supporting` | `false` | Show supporting units too: entry points in a supporting component (`examples`, `scripts`, `tests`, `e2e`, `benchmarks`, ..., plus `tour.supporting` globs) and helper bins (Rust bins under `src/bin/` of a crate with its own main). Hidden ones are counted on the last line |
| `--json` | `false` | Output as JSON |

Text output per unit: name, kind, entry file and the `file:line` that declares it; max impact (`~` when estimated), fragile-critical file count and linked file count; `own` components (linked by this unit only), `links units` (another unit's crate linked in-process), `shared` components (linked by two or more units), each declared link (`declared link: -> | <- | <-> <unit>: <label> [evidence]`) and declared placement from `.heatmap/architecture.yaml`, and each inferred placement with its evidence (`none found` when there is none).

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
| `--json` | `false` | Output as JSON |
| `--fail-on` | (none) | Exit 5 when the PR tier is at or above this tier: `critical`, `high`, `medium`, `low` |
| `--shadow` | `false` | Compute the `--fail-on` decision (default gate `critical`), append it to `.heatmap/shadow.jsonl`, always exit 0 |
| `--author` | HEAD commit author | Email or handle excluded from suggested reviewers |
| `--reviewers` | `3` | Max suggested reviewers; `0` skips the git history lookup |

Uses `git diff --numstat` to identify changed files, then looks up their heat scores. The PR inherits the tier of its highest-scoring file.

A changed file that is not in the heatmap (for example, a file added in this PR) does not default to LOW. It inherits the highest heat score of scored files in its directory, or the nearest parent directory that has any, is marked `"unscored": true` in `--json` output, and adds an `Unscored new file(s)` circuit breaker signal. Docs (`.md`, `.mdx`, `.rst`, `.txt`) are exempt and stay LOW.

Each HIGH or CRITICAL changed file is followed by its top three actions (from its `explanation`); in `--json` they are under `files_changed[].actions`.

**Exit code 3** if git diff fails (error includes valid base examples).

### Two-axis risk

The tier (the most severe tier among touched files) and importance answer "how important is the code this PR touches". A second, independent number answers "how risky is this change itself". Both appear in text output and in `--json`:

| JSON field | Meaning |
|------------|---------|
| `importance` | 0-100, max impact of touched files; files without an LLM assessment count with their heat |
| `change_risk` | 0-100, sum of: lines changed (up to 30), new files (5 each, up to 15), config or infra files such as CI workflows, Dockerfiles, Helm, Terraform, `go.mod` (20), lines deleted from high or critical files (up to 20), low test ratio (up to 15) |
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

A file is hot when its tier is at or above `--min-tier`, or when its LLM-assessed impact is at or above that tier's threshold (61 for `high`, 86 for `critical`), so stable but impactful code is protected even if it rarely changes. A directory with at least two scored files, all hot, becomes one `/dir/` rule; otherwise each hot file gets its own rule. Owners are ranked the same way as `pr check` reviewers. Paths with no owner in git history are written as comments, so they never clear an earlier rule.

The block sits between `# BEGIN highstakes` and `# END highstakes` markers. `--write` replaces that block in place, or appends it to the end of the file, where CODEOWNERS rules take precedence. Lines outside the markers are left alone.

JSON fields: `min_tier`, `entries` (each with `path`, `is_dir`, `tier`, `heat_score`, `files`, `owners`), `block` (the text), `written` (path, empty unless `--write`).

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

The dashboard has four views: a treemap (visual overview), a file explorer (hierarchical tree), an architecture view and a quadrant view. All support tier filtering, search, and a detail panel with both axes, the quadrant guidance and the blast radius reasoning.

The architecture view draws one node per directory (Go package) in dependency layers, entry points on top. Node size is total lines; color is the tier of the highest LLM-assessed impact in the directory (the most severe heat tier when nothing there was assessed), and the number is that impact. A dashed outline and an "N likely" badge mark directories with files likely to need fixes. The label names the hottest file. Test files are left out of nodes and edges. Edges come from the import graph: an arrow from A to B with label N means N files in A import a file in B (same-directory imports are not drawn). Clicking a node lists its files hottest-first in the detail panel; clicking a file there opens its blast radius. Clicking an arrow lists the file-level imports behind it (importer -> imported, with each file's impact; at most 200 per arrow). The layout is computed in the page (layered, with barycenter ordering to reduce crossings; layers wider than about 1500px wrap into extra rows), so the HTML stays self-contained. The `-`, `+`, **Fit** and **1:1** buttons, the mouse wheel and trackpad or touch pinch zoom; drag to pan. The view fits the screen when first opened.

**Group by: Unit** redraws the view by runtime unit (see `list units`): a top row of unit boxes, each holding the components only that unit links, and a band below with the shared libraries, layered by their own imports. Unit boxes are coloured by the max impact of their own files, carry the same fragility marker, and show the inferred placement (Dockerfile name, Helm values key, Kubernetes manifest). Clicking a unit shows its entry point and evidence, placement evidence, totals across everything it links, own components, shared libraries and files with impact, likelihood and quadrant; clicking a shared library shows the units that link it. The hint line counts non-test files no unit links. The toggle is disabled when the heatmap has no units. Supporting units are hidden until **Supporting units** is on (the hint line counts them; the button is disabled when there are none). Links declared in `.heatmap/architecture.yaml` draw as dashed accent arrows labelled "(declared)", with the evidence in the tooltip, and a declared placement replaces the inferred one on the box as "declared: <runs_in>". An invalid `architecture.yaml` makes `dashboard` exit 2.

The quadrant view plots every non-test file with likelihood on x and impact on y. A vertical line marks the likely cutoff and a horizontal line the impact threshold; each quadrant is labelled with its review guidance. Faded dots have an estimated impact. Click a dot to open the file in the detail panel.

## highstakes github install

Generate a GitHub Actions workflow for automated PR triage.

```sh
highstakes github install
```

Creates `.github/workflows/heatmap-triage.yml`.

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
highstakes check calibration --labels FILE [--top 10] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `365d` | Count events in the last N days (format `Nd`) |
| `--holdout` | (off) | Cutoff as `YYYY-MM-DD` or `Nd` ago. Churn and incident history are recomputed from history up to the cutoff, files that did not exist yet are dropped, and events are counted after it. Cannot be combined with `--since` |
| `--events` | `all` | Event kinds to count: `all` (reverts, bug fixes, security fixes, incidents), `fixes` (bug-fix and security commits), `reverts`, or `security` (security fixes only). Incidents count only with `all` |
| `--labels` | (off) | Compare with maintainer ratings in this CSV instead of git history. Cannot be combined with `--since`, `--holdout` or `--events` |
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

### Maintainer labels

`--labels FILE` reads a sheet made by [`create labels`](#highstakes-create-labels) and filled in by maintainers. Rows with a blank rating are skipped; columns are found by header name, so extra columns and any order are fine. For `blast_radius` (LLM-assessed files only) and `heat` it reports, over the rated files in the heatmap:

| Metric | Definition |
|--------|------------|
| Spearman | Rank correlation between the score and the 1-5 rating |
| precision@K | Share of the K highest-scoring rated files with a rating of 4 or 5 |
| Lift | precision@K divided by the share of rated files with 4 or 5 |

It also prints how many files were rated, how many are in the heatmap, and the rated paths it could not find. Under 10 rated files in the heatmap it prints a `WARNING` (`too_few` in JSON). JSON fields: `rated`, `matched`, `unmatched`, `positives`, `base_rate`, `k`, `too_few`, `signals` (each `name`, `available`, `note`, `files`, `precision_at_k`, `lift`, `spearman`).

**Exit code 2** for invalid flags (bad `--since`/`--holdout` format, a future cutoff, `--top` below 1, both `--since` and `--holdout`, an unknown `--events` value (the error lists the valid ones), `--labels` with a history flag) or a malformed labels CSV (the error names the line: a rating outside 1-5, a duplicate path, a missing `path` or `maintainer_rating` column). **Exit code 3** if git history cannot be read. **Exit code 4** if no heatmap or labels file exists.

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
