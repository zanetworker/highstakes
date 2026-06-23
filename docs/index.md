---
layout: default
title: Code Heatmap
description: Know which code would hurt the most if it broke
---

# Code Heatmap

**LLM-assisted blast radius analysis for every file in your repo.**

AI writes code faster than you can review it. Code Heatmap tells you where you're needed and where AI can handle the rest.

<p align="center">
  <img src="images/treemap-view.png" width="800" alt="Treemap view">
</p>

## What It Does

Reads every source file, asks an LLM "if this breaks, what's the blast radius?", and scores it across security, data, availability, and user impact. Surfaces reasoning so you know *why* a file is critical.

```
$ highstakes get python/openshell/sandbox.py

python/openshell/sandbox.py  🔥🔥 HIGH  (score: 73)

Blast Radius (LLM-assessed):
  Sandbox management failures could compromise isolation,
  leading to security breaches or service outage.
  Reason: Sandbox isolation is a security boundary.
  Security: 95  Data: 90  Availability: 90  User: 90

Review: 2 reviewers, ~45 min, auto-merge blocked
```

## Install

```sh
go install github.com/zanetworker/highstakes/cmd/heatmap@latest
export OPENROUTER_API_KEY="sk-or-..."
```

## First Analysis

```sh
cd /path/to/repo
highstakes init && highstakes analyze
highstakes dashboard    # Visual treemap + explorer
```

## Documentation

- [Getting Started](getting-started.md)
- [Configuration](configuration.md)
- [CI Integration](ci-integration.md)
- [CLI Reference](cli-reference.md)
- [How It Works](how-it-works.md)

## Tier System

| Tier | Score | Review Required |
|------|-------|-----------------|
| 🔥🔥🔥 CRITICAL | 86-100 | 2 senior reviewers + security scan |
| 🔥🔥 HIGH | 61-85 | 2 reviewers + integration tests |
| 🔥 MEDIUM | 31-60 | 1 reviewer |
| 🟢 LOW | 0-30 | Auto-review safe |

## Dashboard

Two views: a treemap for visual overview and a file explorer with directory hierarchy.

<p align="center">
  <img src="images/explorer-view.png" width="800" alt="Explorer view">
</p>
