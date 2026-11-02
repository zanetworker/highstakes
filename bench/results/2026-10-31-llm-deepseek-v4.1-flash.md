# HighStakes calibration benchmark (2026-10-31, LLM: deepseek-v4.1-flash)

Mode: LLM. Binary: `highstakes version 1.0.0`. K = 10.
Event windows are anchored to each pinned commit: in-sample counts events in the 365 days before it, holdout scores with history up to the cutoff (commit date minus the repo holdout days, default 180) and counts events after it. Primary repos are the use cases HighStakes is tuned for; general repos guard against overfitting to them.

## Heat score

| Repo | Tier | Language | Files | Wall (s) | Mode | Base rate | Heat Spearman | Precision@10 | Lift |
|---|---|---|---:|---:|---|---:|---:|---:|---:|
| openshell | primary | Rust | 1433 | 145 | in-sample | 41.9% | 0.27 | 1.00 | 2.38 |
| openshell | primary | Rust | 931 | 145 | holdout | 32.1% | 0.32 | 0.92 | 2.85 |
| openclaw-enterprise | primary | TypeScript | 1585 | 215 | in-sample | 66.2% | 0.24 | 1.00 | 1.51 |
| openclaw-enterprise | primary | TypeScript | 720 | 215 | holdout | 62.8% | 0.28 | 1.00 | 1.59 |
| caddy | general | Go | 245 | 22 | in-sample | 32.7% | 0.43 | 0.53 | 1.61 |
| caddy | general | Go | 231 | 22 | holdout | 24.2% | 0.37 | 0.38 | 1.55 |
| tokio | general | Rust | 843 | 72 | in-sample | 14.7% | 0.26 | 0.45 | 3.06 |
| tokio | general | Rust | 804 | 72 | holdout | 9.3% | 0.25 | 0.45 | 4.82 |
| hono | general | TypeScript | 383 | 32 | in-sample | 29.5% | 0.45 | 0.90 | 3.05 |
| hono | general | TypeScript | 304 | 32 | holdout | 26.3% | 0.51 | 0.96 | 3.65 |
| scrapy | general | Python | 376 | 38 | in-sample | 29.5% | 0.39 | 0.68 | 2.30 |
| scrapy | general | Python | 300 | 38 | holdout | 27.7% | 0.42 | 0.65 | 2.35 |
| commons-lang | general | Java | 636 | 54 | in-sample | 17.9% | 0.37 | 0.90 | 5.02 |
| commons-lang | general | Java | 540 | 54 | holdout | 13.3% | 0.32 | 0.60 | 4.50 |

## Averages by tier

Means over repos in the tier; `n/a` values are left out of a mean.

| Tier | Mode | Repos | Heat Spearman | Precision@10 | Lift | Churn | Complexity | Centrality | Blast radius |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| primary | in-sample | 2 | 0.25 | 1.00 | 1.95 | 0.72 | 0.33 | 0.09 | 0.11 |
| primary | holdout | 2 | 0.30 | 0.96 | 2.22 | 0.50 | 0.35 | 0.18 | 0.16 |
| general | in-sample | 5 | 0.38 | 0.69 | 3.01 | 0.50 | 0.38 | 0.19 | 0.29 |
| general | holdout | 5 | 0.38 | 0.61 | 3.37 | 0.31 | 0.40 | 0.19 | 0.29 |

## Per-factor Spearman (vs event count)

| Repo | Tier | Language | Mode | Heat | Churn | Complexity | Centrality | Blast radius |
|---|---|---|---|---:|---:|---:|---:|---:|
| openshell | primary | Rust | in-sample | 0.27 | 0.60 | 0.26 | 0.19 | 0.20 |
| openshell | primary | Rust | holdout | 0.32 | 0.45 | 0.28 | 0.23 | 0.25 |
| openclaw-enterprise | primary | TypeScript | in-sample | 0.24 | 0.84 | 0.41 | 0.00 | 0.01 |
| openclaw-enterprise | primary | TypeScript | holdout | 0.28 | 0.54 | 0.43 | 0.13 | 0.08 |
| caddy | general | Go | in-sample | 0.43 | 0.57 | 0.44 | 0.16 | 0.37 |
| caddy | general | Go | holdout | 0.37 | 0.37 | 0.38 | 0.14 | 0.32 |
| tokio | general | Rust | in-sample | 0.26 | 0.35 | 0.21 | 0.15 | 0.21 |
| tokio | general | Rust | holdout | 0.25 | 0.25 | 0.21 | 0.17 | 0.20 |
| hono | general | TypeScript | in-sample | 0.45 | 0.58 | 0.44 | 0.18 | 0.31 |
| hono | general | TypeScript | holdout | 0.51 | 0.36 | 0.59 | 0.21 | 0.34 |
| scrapy | general | Python | in-sample | 0.39 | 0.46 | 0.38 | 0.31 | 0.36 |
| scrapy | general | Python | holdout | 0.42 | 0.36 | 0.40 | 0.34 | 0.40 |
| commons-lang | general | Java | in-sample | 0.37 | 0.52 | 0.43 | 0.17 | 0.23 |
| commons-lang | general | Java | holdout | 0.32 | 0.21 | 0.43 | 0.09 | 0.17 |

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
