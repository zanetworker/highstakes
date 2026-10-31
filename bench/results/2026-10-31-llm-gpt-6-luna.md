# HighStakes calibration benchmark (2026-10-31, LLM: gpt-6-luna)

Mode: LLM. Binary: `highstakes version 1.0.0`. K = 10.
Event windows are anchored to each pinned commit: in-sample counts events in the 365 days before it, holdout scores with history up to the cutoff (commit date minus the repo holdout days, default 180) and counts events after it. Primary repos are the use cases HighStakes is tuned for; general repos guard against overfitting to them.

## Heat score

| Repo | Tier | Language | Files | Wall (s) | Mode | Base rate | Heat Spearman | Precision@10 | Lift |
|---|---|---|---:|---:|---|---:|---:|---:|---:|
| openshell | primary | Rust | 1433 | 248 | in-sample | 41.9% | 0.31 | 0.92 | 2.19 |
| openshell | primary | Rust | 931 | 248 | holdout | 32.1% | 0.35 | 1.00 | 3.11 |
| openclaw-enterprise | primary | TypeScript | 1585 | 340 | in-sample | 66.2% | 0.34 | 1.00 | 1.51 |
| openclaw-enterprise | primary | TypeScript | 720 | 340 | holdout | 62.8% | 0.38 | 1.00 | 1.59 |
| caddy | general | Go | 245 | 45 | in-sample | 32.7% | 0.42 | 0.70 | 2.14 |
| caddy | general | Go | 231 | 45 | holdout | 24.2% | 0.36 | 0.60 | 2.47 |
| tokio | general | Rust | 843 | 136 | in-sample | 14.7% | 0.28 | 0.73 | 4.99 |
| tokio | general | Rust | 804 | 136 | holdout | 9.3% | 0.27 | 0.60 | 6.43 |
| hono | general | TypeScript | 383 | 66 | in-sample | 29.5% | 0.48 | 0.95 | 3.22 |
| hono | general | TypeScript | 304 | 66 | holdout | 26.3% | 0.52 | 0.83 | 3.17 |
| scrapy | general | Python | 376 | 66 | in-sample | 29.5% | 0.38 | 0.80 | 2.71 |
| scrapy | general | Python | 300 | 66 | holdout | 27.7% | 0.40 | 0.65 | 2.35 |
| commons-lang | general | Java | 636 | 106 | in-sample | 17.9% | 0.41 | 0.53 | 2.93 |
| commons-lang | general | Java | 540 | 106 | holdout | 13.3% | 0.37 | 0.45 | 3.38 |

## Averages by tier

Means over repos in the tier; `n/a` values are left out of a mean.

| Tier | Mode | Repos | Heat Spearman | Precision@10 | Lift | Churn | Complexity | Centrality | Blast radius |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| primary | in-sample | 2 | 0.33 | 0.96 | 1.85 | 0.72 | 0.34 | 0.09 | 0.18 |
| primary | holdout | 2 | 0.37 | 1.00 | 2.35 | 0.50 | 0.36 | 0.18 | 0.23 |
| general | in-sample | 5 | 0.39 | 0.74 | 3.20 | 0.50 | 0.38 | 0.20 | 0.30 |
| general | holdout | 5 | 0.38 | 0.63 | 3.56 | 0.31 | 0.40 | 0.19 | 0.28 |

## Per-factor Spearman (vs event count)

| Repo | Tier | Language | Mode | Heat | Churn | Complexity | Centrality | Blast radius |
|---|---|---|---|---:|---:|---:|---:|---:|
| openshell | primary | Rust | in-sample | 0.31 | 0.60 | 0.26 | 0.19 | 0.23 |
| openshell | primary | Rust | holdout | 0.35 | 0.45 | 0.28 | 0.23 | 0.27 |
| openclaw-enterprise | primary | TypeScript | in-sample | 0.34 | 0.84 | 0.41 | 0.00 | 0.13 |
| openclaw-enterprise | primary | TypeScript | holdout | 0.38 | 0.54 | 0.44 | 0.14 | 0.18 |
| caddy | general | Go | in-sample | 0.42 | 0.57 | 0.44 | 0.16 | 0.36 |
| caddy | general | Go | holdout | 0.36 | 0.37 | 0.38 | 0.14 | 0.31 |
| tokio | general | Rust | in-sample | 0.28 | 0.35 | 0.21 | 0.15 | 0.22 |
| tokio | general | Rust | holdout | 0.27 | 0.25 | 0.21 | 0.17 | 0.22 |
| hono | general | TypeScript | in-sample | 0.48 | 0.58 | 0.44 | 0.18 | 0.31 |
| hono | general | TypeScript | holdout | 0.52 | 0.36 | 0.59 | 0.21 | 0.32 |
| scrapy | general | Python | in-sample | 0.38 | 0.46 | 0.38 | 0.31 | 0.32 |
| scrapy | general | Python | holdout | 0.40 | 0.36 | 0.40 | 0.35 | 0.35 |
| commons-lang | general | Java | in-sample | 0.41 | 0.52 | 0.43 | 0.17 | 0.25 |
| commons-lang | general | Java | holdout | 0.37 | 0.21 | 0.43 | 0.09 | 0.19 |

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
