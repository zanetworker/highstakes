# Calibration benchmark

A fixed set of repos used to judge scoring changes across projects and languages instead of on a single repo. Each run analyzes every repo at a pinned commit and compares the heat scores with the fix history using `highstakes check calibration`.

## Repos

Listed in [repos.yaml](repos.yaml), each pinned to a commit SHA and given a tier:

- `primary`: the real use cases HighStakes is tuned for. Listed first and averaged separately.
- `general`: one well-known public repo per language. These guard against tuning weights that only work on the primary repos.
- `later`: candidates marked `enabled: false`, skipped unless you pass `--include-disabled`.

| Repo | Tier | Language | Why |
|---|---|---|---|
| [NVIDIA/OpenShell](https://github.com/NVIDIA/OpenShell) | primary | Rust | Real use case. Young repo (about 3 months of history), so it uses a 45-day holdout. |
| [openclaw/openclaw-enterprise](https://github.com/openclaw/openclaw-enterprise) | primary | TypeScript | Real use case. About 2 months of history, so it uses a 30-day holdout. |
| [caddyserver/caddy](https://github.com/caddyserver/caddy) | general | Go | About 420 files, history since 2015, steady "fix" commits per module, Apache-2.0. |
| [tokio-rs/tokio](https://github.com/tokio-rs/tokio) | general | Rust | About 840 files, history since 2016, conventional `fix:` style messages, MIT. |
| [honojs/hono](https://github.com/honojs/hono) | general | TypeScript | About 490 files, history since 2021, conventional commits (`fix(...)`), MIT. |
| [scrapy/scrapy](https://github.com/scrapy/scrapy) | general | Python | About 550 files, history since 2008, many fix and revert commits, BSD-3-Clause. |
| [apache/commons-lang](https://github.com/apache/commons-lang) | general | Java | About 640 files, history since 2002, fix commits tied to LANG issues, Apache-2.0. |
| [openclaw/openclaw](https://github.com/openclaw/openclaw) | later | TypeScript | Disabled: over 50,000 analyzable files and 120,000 commits, too large for a routine run. |

Selection rules for general repos: 300 to 3,000 analyzable files (so a no-LLM run takes seconds and a paid LLM run stays cheap), at least 3 years of history, many fix commits, a permissive license, no monorepos or vendored code.

## Run it

```sh
bench/run.sh                    # no LLM: static analysis and git history only
bench/run.sh --bin ./highstakes # use an existing binary instead of building one
bench/run.sh --cache-dir /data/hs-bench
bench/run.sh --include-disabled # also run repos with enabled: false
bench/run.sh --llm              # paid: assesses blast radius with the LLM
bench/run.sh --source prs       # events from merged GitHub PRs (needs gh, logged in)
```

Requirements: `git`, `jq`, `awk`, and Go when `--bin` is not given; `gh` for `--source prs`. `--llm` needs `OPENROUTER_API_KEY` or `HIGHSTAKES_API_KEY`, prints the estimated file count, and asks before it continues when run from a terminal.

What it does:

1. Clones each enabled repo with full history into the cache dir (default `$HIGHSTAKES_BENCH_CACHE`, else `$XDG_CACHE_HOME/highstakes-bench`, else `~/Library/Caches/highstakes-bench` on macOS or `~/.cache/highstakes-bench`). The cache dir must be outside this repo. When an entry has `local_mirror` and that directory is a git clone, the script clones from it instead of the network (and fetches its remote-tracking refs if the pin is only there), then points the cache clone's `origin` at `url`. The mirror is only read, never checked out or analyzed in place.
2. Checks out the pinned commit and prints an estimated file count per repo.
3. Runs `highstakes analyze --no-llm` (or with the LLM under `--llm`), then `highstakes check calibration --json` in-sample and with a holdout cutoff.
4. Writes `bench/results/<date>.md` and `bench/results/<date>.json` (`<date>-prs.*` with `--source prs`). Raw per-repo output stays in `<cache dir>/raw/`.

Reruns are safe: existing clones are reused and fetched only when the pinned commit is missing, and results for the same date are overwritten.

The event windows are anchored to each pinned commit, not to today. In-sample counts events in the 365 days before the commit. Holdout scores files with history up to a cutoff `holdout_days` before the commit (default 180) and counts events after it. One part still drifts: `analyze` computes churn relative to today, so in-sample heat changes slowly as the pinned commits age. The holdout numbers do not drift. Move the pins forward when you rebaseline.

To add or bump a repo, edit `repos.yaml` and keep its flat layout (`- name:` followed by indented `tier`, `url`, `language`, `commit` and the optional `local_mirror`, `holdout_days`, `enabled`, `github`), since the script reads it with awk.

## Read the results

The heat table has one row per repo and mode, primary repos first. The averages table gives the mean of each metric per tier and mode, so a change that helps the primary repos but hurts the general ones is visible at a glance.

| Column | Meaning |
|---|---|
| Files | Files scored. Holdout counts only files that existed at the cutoff. |
| Base rate | Share of files with at least one event (revert, bug fix, security fix, incident). |
| Heat Spearman | Rank correlation between heat score and event count. 0 is no relation, 1 is a perfect ranking. |
| Precision@10 | Share of the 10 hottest files that had an event. |
| Lift | Precision@10 divided by the base rate. 1.0 is no better than picking files at random. |

The per-factor table gives the same Spearman for each signal alone: churn, complexity, centrality, and blast radius (only when the LLM ran). A factor shows `n/a` when it is not available or is the same for every file, so the ranking is undefined. Use it to choose weights: a factor that correlates in every language deserves weight, one that only works in one language points to a parser gap rather than a bad signal.

With `--source prs`, the calibration runs use merged PRs (`--source prs --repo <github>`, see [Pull-request source](../docs/cli-reference.md#pull-request-source)) and the report adds two tables: likelihood against bug-fix and revert PRs on the holdout window next to the commit-keyword source, and blast radius against each stakes signal (backported, fast revert, severity label, review depth, CODEOWNERS, combined) in-sample, plus the `pr_activity` reference row (all PRs), which separates maintainer judgment from plain activity. PR metadata is cached per clone in `.heatmap/cache/github/`, so reruns cost few API calls. Signals show `n/a` when the clone lacks the data (no release branches, no CODEOWNERS).

Prefer the holdout rows. In-sample churn is computed from the same commits the events come from, so churn looks better there than it is.

## Caveats

- Fix history measures how likely code is to break, not the blast radius if it does. A weak blast-radius correlation is expected and is not by itself a failure; judge blast radius with reviewers, not this benchmark.
- To check blast radius against history, run `highstakes check calibration --events security --since 1095d` (and `--holdout <cutoff> --events security`) in a clone with an LLM heatmap. Security fixes are a rough proxy for impact, but they are rare: most benchmark repos have fewer than 10 files with one, and the report warns when that happens. For a direct check, have maintainers fill a `highstakes create labels` sheet and score it with `--labels`.
- `--source prs` replaces keywords with PR labels and titles, which depend on each project's labelling habits; review depth and CODEOWNERS also rise with code volume and activity.
- Events come from commit-message keywords, so they are noisy: "fix typo" counts as a fix, and a fix that touches ten files labels all ten.
- Complexity, centrality and blast radius come from today's file contents even in holdout mode, so some leakage remains.
- Precision@10 on a few hundred files moves in steps of 0.1. Treat differences under about 0.1 as noise.
