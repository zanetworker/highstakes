# HighStakes calibration benchmark (2026-10-31, LLM: claude-haiku-5.5)

Mode: LLM. Binary: `highstakes version 1.0.0`. K = 10.
Event windows are anchored to each pinned commit: in-sample counts events in the 365 days before it, holdout scores with history up to the cutoff (commit date minus the repo holdout days, default 180) and counts events after it. Primary repos are the use cases HighStakes is tuned for; general repos guard against overfitting to them.

## Heat score

| Repo | Tier | Language | Files | Wall (s) | Mode | Base rate | Heat Spearman | Precision@10 | Lift |
|---|---|---|---:|---:|---|---:|---:|---:|---:|
| openshell | primary | Rust | 1433 | 207 | in-sample | 41.9% | 0.31 | 1.00 | 2.38 |
| openshell | primary | Rust | 931 | 207 | holdout | 32.1% | 0.32 | 1.00 | 3.11 |
| openclaw-enterprise | primary | TypeScript | 1585 | 288 | in-sample | 66.2% | 0.29 | 1.00 | 1.51 |
| openclaw-enterprise | primary | TypeScript | 720 | 288 | holdout | 62.8% | 0.34 | 1.00 | 1.59 |
| caddy | general | Go | 245 | 37 | in-sample | 32.7% | 0.42 | 0.60 | 1.84 |
| caddy | general | Go | 231 | 37 | holdout | 24.2% | 0.33 | 0.53 | 2.20 |
| tokio | general | Rust | 843 | 116 | in-sample | 14.7% | 0.27 | 0.67 | 4.53 |
| tokio | general | Rust | 804 | 116 | holdout | 9.3% | 0.26 | 0.53 | 5.63 |
| hono | general | TypeScript | 383 | 53 | in-sample | 29.5% | 0.44 | 1.00 | 3.39 |
| hono | general | TypeScript | 304 | 53 | holdout | 26.3% | 0.51 | 0.90 | 3.42 |
| scrapy | general | Python | 376 | 53 | in-sample | 29.5% | 0.37 | 0.60 | 2.03 |
| scrapy | general | Python | 300 | 53 | holdout | 27.7% | 0.39 | 0.53 | 1.93 |
| commons-lang | general | Java | 636 | 86 | in-sample | 17.9% | 0.41 | 0.85 | 4.74 |
| commons-lang | general | Java | 540 | 86 | holdout | 13.3% | 0.39 | 0.60 | 4.50 |

## Averages by tier

Means over repos in the tier; `n/a` values are left out of a mean.

| Tier | Mode | Repos | Heat Spearman | Precision@10 | Lift | Churn | Complexity | Centrality | Blast radius |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| primary | in-sample | 2 | 0.30 | 1.00 | 1.95 | 0.72 | 0.34 | 0.09 | 0.16 |
| primary | holdout | 2 | 0.33 | 1.00 | 2.35 | 0.50 | 0.36 | 0.18 | 0.20 |
| general | in-sample | 5 | 0.38 | 0.74 | 3.31 | 0.50 | 0.38 | 0.19 | 0.29 |
| general | holdout | 5 | 0.38 | 0.62 | 3.54 | 0.31 | 0.40 | 0.19 | 0.28 |

## Per-factor Spearman (vs event count)

| Repo | Tier | Language | Mode | Heat | Churn | Complexity | Centrality | Blast radius |
|---|---|---|---|---:|---:|---:|---:|---:|
| openshell | primary | Rust | in-sample | 0.31 | 0.60 | 0.26 | 0.19 | 0.23 |
| openshell | primary | Rust | holdout | 0.32 | 0.45 | 0.28 | 0.23 | 0.25 |
| openclaw-enterprise | primary | TypeScript | in-sample | 0.29 | 0.84 | 0.41 | 0.00 | 0.08 |
| openclaw-enterprise | primary | TypeScript | holdout | 0.34 | 0.54 | 0.44 | 0.14 | 0.14 |
| caddy | general | Go | in-sample | 0.42 | 0.57 | 0.44 | 0.16 | 0.37 |
| caddy | general | Go | holdout | 0.33 | 0.37 | 0.38 | 0.14 | 0.27 |
| tokio | general | Rust | in-sample | 0.27 | 0.35 | 0.21 | 0.15 | 0.23 |
| tokio | general | Rust | holdout | 0.26 | 0.25 | 0.21 | 0.17 | 0.21 |
| hono | general | TypeScript | in-sample | 0.44 | 0.58 | 0.44 | 0.18 | 0.33 |
| hono | general | TypeScript | holdout | 0.51 | 0.36 | 0.59 | 0.21 | 0.38 |
| scrapy | general | Python | in-sample | 0.37 | 0.46 | 0.38 | 0.31 | 0.30 |
| scrapy | general | Python | holdout | 0.39 | 0.36 | 0.40 | 0.34 | 0.34 |
| commons-lang | general | Java | in-sample | 0.41 | 0.52 | 0.43 | 0.17 | 0.22 |
| commons-lang | general | Java | holdout | 0.39 | 0.21 | 0.43 | 0.09 | 0.19 |

## Repos

| Repo | Commit | Commit date | Holdout cutoff |
|---|---|---|---|
| [openshell](https://github.com/NVIDIA/OpenShell) | `4d4c15c133d4` | 2026-10-29 | 2026-09-14 |
| [openclaw-enterprise](https://github.com/openclaw/openclaw-enterprise) | `20133c2b8bc1` | 2026-10-29 | 2026-09-29 |
| [caddy](https://github.com/caddyserver/caddy) | `eb04ed52be5e` | 2026-10-30 | 2026-05-03 |
| [tokio](https://github.com/tokio-rs/tokio) | `6470b5e4b8e9` | 2026-10-29 | 2026-05-02 |
| [hono](https://github.com/honojs/hono) | `17ad0e21f5a9` | 2026-10-30 | 2026-05-03 |
| [scrapy](https://github.com/scrapy/scrapy) | `51c10b14fca6` | 2026-10-30 | 2026-05-03 |
| [commons-lang](https://github.com/apache/commons-lang) | `61837eb4472d` | 2026-10-30 | 2026-05-03 |

Fix history measures how likely code is to break, not the blast radius if it does. See bench/README.md.
