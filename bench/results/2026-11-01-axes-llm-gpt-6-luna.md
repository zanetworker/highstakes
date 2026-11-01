# HighStakes calibration benchmark (2026-11-01, two axes, whole-word fix detection, LLM: gpt-6-luna)

Mode: LLM. Binary: `highstakes version 1.0.0`. K = 10.
Event windows are anchored to each pinned commit: in-sample counts events in the 365 days before it, holdout scores with history up to the cutoff (commit date minus the repo holdout days, default 180) and counts events after it. Primary repos are the use cases HighStakes is tuned for; general repos guard against overfitting to them.

## Heat score

| Repo | Tier | Language | Files | Wall (s) | Mode | Base rate | Heat Spearman | Precision@10 | Lift |
|---|---|---|---:|---:|---|---:|---:|---:|---:|
| openshell | primary | Rust | 1432 | 32 | in-sample | 40.5% | 0.27 | 0.67 | 1.65 |
| openshell | primary | Rust | 931 | 32 | holdout | 31.7% | 0.29 | 0.85 | 2.69 |
| openclaw-enterprise | primary | TypeScript | 1185 | 86 | in-sample | 56.9% | 0.34 | 0.50 | 0.88 |
| openclaw-enterprise | primary | TypeScript | 532 | 86 | holdout | 53.8% | 0.42 | 0.95 | 1.77 |
| caddy | general | Go | 245 | 3 | in-sample | 31.4% | 0.34 | 0.70 | 2.23 |
| caddy | general | Go | 231 | 3 | holdout | 23.8% | 0.31 | 0.60 | 2.52 |
| tokio | general | Rust | 843 | 6 | in-sample | 13.9% | 0.27 | 0.59 | 4.22 |
| tokio | general | Rust | 804 | 6 | holdout | 8.7% | 0.26 | 0.49 | 5.58 |
| hono | general | TypeScript | 358 | 4 | in-sample | 28.2% | 0.42 | 0.88 | 3.12 |
| hono | general | TypeScript | 279 | 4 | holdout | 24.7% | 0.45 | 0.68 | 2.75 |
| scrapy | general | Python | 376 | 10 | in-sample | 26.6% | 0.40 | 0.80 | 3.01 |
| scrapy | general | Python | 300 | 10 | holdout | 25.7% | 0.42 | 0.67 | 2.60 |
| commons-lang | general | Java | 312 | 9 | in-sample | 21.2% | 0.36 | 0.42 | 1.99 |
| commons-lang | general | Java | 304 | 9 | holdout | 11.8% | 0.39 | 0.28 | 2.36 |

## Averages by tier

Means over repos in the tier; `n/a` values are left out of a mean.

| Tier | Mode | Repos | Heat Spearman | Precision@10 | Lift | Churn | Complexity | Centrality | Blast radius |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| primary | in-sample | 2 | 0.30 | 0.58 | 1.26 | 0.69 | 0.29 | 0.18 | 0.27 |
| primary | holdout | 2 | 0.35 | 0.90 | 2.23 | 0.47 | 0.30 | 0.28 | 0.34 |
| general | in-sample | 5 | 0.36 | 0.68 | 2.91 | 0.48 | 0.37 | 0.21 | 0.32 |
| general | holdout | 5 | 0.37 | 0.54 | 3.16 | 0.32 | 0.40 | 0.23 | 0.34 |

## Per-factor Spearman (vs event count)

| Repo | Tier | Language | Mode | Heat | Churn | Complexity | Centrality | Blast radius |
|---|---|---|---|---:|---:|---:|---:|---:|
| openshell | primary | Rust | in-sample | 0.27 | 0.59 | 0.27 | 0.19 | 0.23 |
| openshell | primary | Rust | holdout | 0.29 | 0.46 | 0.28 | 0.23 | 0.27 |
| openclaw-enterprise | primary | TypeScript | in-sample | 0.34 | 0.79 | 0.30 | 0.17 | 0.31 |
| openclaw-enterprise | primary | TypeScript | holdout | 0.42 | 0.48 | 0.31 | 0.32 | 0.41 |
| caddy | general | Go | in-sample | 0.34 | 0.54 | 0.42 | 0.16 | 0.34 |
| caddy | general | Go | holdout | 0.31 | 0.37 | 0.37 | 0.15 | 0.31 |
| tokio | general | Rust | in-sample | 0.27 | 0.33 | 0.19 | 0.16 | 0.22 |
| tokio | general | Rust | holdout | 0.26 | 0.22 | 0.19 | 0.17 | 0.22 |
| hono | general | TypeScript | in-sample | 0.42 | 0.55 | 0.41 | 0.25 | 0.42 |
| hono | general | TypeScript | holdout | 0.45 | 0.36 | 0.58 | 0.30 | 0.45 |
| scrapy | general | Python | in-sample | 0.40 | 0.50 | 0.39 | 0.33 | 0.37 |
| scrapy | general | Python | holdout | 0.42 | 0.39 | 0.42 | 0.36 | 0.40 |
| commons-lang | general | Java | in-sample | 0.36 | 0.49 | 0.44 | 0.18 | 0.27 |
| commons-lang | general | Java | holdout | 0.39 | 0.24 | 0.43 | 0.17 | 0.34 |

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
