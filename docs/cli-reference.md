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

Get the heat score and blast radius reasoning for a specific file.

```sh
highstakes get <file> [--json]
```

If the file is not found, suggests similar paths from the heatmap.

**Exit code 4** if file not found.

## highstakes get tour

Get a risk-ordered reading path through the repository. Makes no LLM calls; reads only `.heatmap/heatmap.json` and the source files (for line anchors).

```sh
highstakes get tour [--limit N] [--focus <dir|file>] [--json]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | `0` | Keep the N hottest files (0 = all). Selection happens before ordering |
| `--focus` | `""` | Only files under this repo-relative directory, or this one file. Matches whole path segments (`api` does not match `apiv2`) |
| `--json` | `false` | Output as JSON |

Ordering:

1. Files are grouped into sections by directory (one Go package per section).
2. Sections are ordered by dependency level: level 0 imports no other directory, level N imports something at level N-1. Levels come from the whole repo's import graph, so the order holds even when `--limit` drops the directories in between. Directories in an import cycle share a level.
3. Within a level, sections go hottest first; within a section, files go hottest first.

Test files (`*_test.go`, `*.test.ts`, `*.spec.js`, `test_*.py` and similar) are left out of the tour and of the dependency order: they import everything and say little about where the stakes are.

`tour` is a subcommand of `get`, so `highstakes get tour` runs the tour and `highstakes get <file>` still looks up a file. A file literally named `tour` at the repo root cannot be looked up with `get`; use `list` instead.

**Exit code 2** for a negative `--limit`. **Exit code 4** if `--focus` matches no file.

JSON schema (version `1`), shaped for `/show-me walkthrough` and coding agents: sections are the grouped narrative, stops carry `file:line` anchors.

```json
{
  "version": "1",
  "repo": "/path/to/repo",
  "commit": "abc1234",
  "branch": "main",
  "analyzed_at": "2026-10-30T12:37:55+01:00",
  "focus": "internal",
  "ordering": "sections by dependency level (dependencies first), then max heat; stops by heat",
  "total_stops": 8,
  "sections": [
    {
      "order": 1,
      "title": "internal/types",
      "dir": "internal/types",
      "level": 0,
      "max_heat": 31,
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
| `sections[].level` | Dependency depth of the directory; 0 = imports no other directory |
| `sections[].depends_on` / `used_by` | Directories this one imports / that import it |
| `stops[].order` | Global 1-based reading position |
| `stops[].line`, `anchor` | First top-level declaration (`func`, `type`, `class`, `def`, `export`, `pub`, ...), else 1 |
| `stops[].why` | `critical_reason`, else `summary`, else static signals (importers, recent commits, incidents) |
| `stops[].top_impact` | Highest LLM impact dimension: `security`, `data`, `availability` or `user`; omitted when not assessed |
| `stops[].depends_on` / `used_by` | Up to 5 files, hottest first; `*_count` is the full number |
| `stops[].owners` | Annotation owner first, then recent commit authors by commit count; up to 3 |

## highstakes list

List files sorted by heat score.

```sh
highstakes list [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--tier` | (all) | Filter by tier: `critical`, `high`, `medium`, `low` |
| `--limit` | `0` | Maximum files to return (0 = all) |
| `--json` | `false` | Output as JSON |

**Exit code 2** if `--tier` value is invalid (error includes valid values).

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

**Exit code 3** if git diff fails (error includes valid base examples).

### Two-axis risk

The tier answers "how important is the code this PR touches". A second, independent number answers "how risky is this change itself". Both appear in text output and in `--json`:

| JSON field | Meaning |
|------------|---------|
| `importance` | 0-100, max heat of touched files (same value as `heat_score`) |
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

A directory with at least two scored files, all at or above `--min-tier`, becomes one `/dir/` rule; otherwise each hot file gets its own rule. Owners are ranked the same way as `pr check` reviewers. Paths with no owner in git history are written as comments, so they never clear an earlier rule.

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

Prints YAML by default. Fields: `version`, `generated_by`, `min_tier`, `description`, `paths` (globs: `dir/**` for fully hot directories, exact paths otherwise) and `files` (every hot file with `path`, `tier`, `heat_score` and the LLM `reason` when available). Feed `paths` to the path restrictions of Copilot code review, CodeRabbit path instructions, or your own approval bot.

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

The dashboard has three views: a treemap (visual overview), a file explorer (hierarchical tree) and an architecture view. All support tier filtering, search, and a detail panel with blast radius reasoning.

The architecture view draws one node per directory (Go package) in dependency layers, entry points on top. Node size is total lines, color is the most severe tier in the directory, and the label names the hottest file. Test files are left out of nodes and edges. Edges come from the import graph: an arrow from A to B with label N means N files in A import a file in B (same-directory imports are not drawn). Clicking a node lists its files hottest-first in the detail panel; clicking a file there opens its blast radius. The layout is computed in the page (layered, with barycenter ordering to reduce crossings), so the HTML stays self-contained.

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

**Events.** Each non-merge commit in the window is labelled from its message: a revert (subject starts with `Revert` or the body has `This reverts commit`), a bug fix (fix, bug, patch, hotfix, repair, correct or resolve in the subject), or a security fix (`CVE-` or `GHSA-` anywhere in the message, or `security` as a standalone word or a word starting with `vuln` in the subject). The security keywords do not count in `docs`, `ci`, `test`, `chore`, `build` or `style` commits, nor inside names such as `Content-Security-Policy`, `podSecurityContext`, `SecurityException` or `SECURITY.md`. Every file the commit touched gets one event. Commits touching more than 50 files are skipped as bulk changes. Incidents in `.heatmap/incidents.json` dated in the window add one event to their file.

**Metrics.**

| Metric | Definition |
|--------|------------|
| Base rate | Share of scored files with at least one event |
| Hit rate (per tier) | Share of files in the tier with at least one event; `events/file` is the mean event count |
| precision@K | Share of the K highest-scoring files with at least one event. Files tied at the K-th score share the remaining slots, so ties give the average over every ordering |
| recall@K | Share of all files with events that are in the top K |
| Lift | precision@K divided by the base rate; 1.0 means no better than a random pick |
| Spearman | Rank correlation between the score and the event count, with average ranks for ties; `n/a` when a side has no variation |

Metrics are reported for `heat` and for `blast_radius` (LLM-assessed files only), `churn`, `complexity` and `centrality` alone. Surprises list LOW-tier files with the most events and CRITICAL/HIGH files with none.

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
