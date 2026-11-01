# CI Integration

Code Heatmap can run in any CI system to automatically assess PR risk and gate merges on high-risk files.

## GitHub Actions

### Setup

Copy [`.github/workflows/heatmap-triage.yml`](https://github.com/zanetworker/highstakes/blob/main/.github/workflows/heatmap-triage.yml) into your repo. Each run:

1. Runs `highstakes init` only when `.heatmap/config.yaml` is missing (a committed config is kept), then `highstakes analyze` (cached by file content).
2. Collects the reviewers whose latest review is an approval, with `gh api repos/<repo>/pulls/<n>/reviews`.
3. Runs `highstakes pr check --format markdown --fail-on critical --approvals <logins> --repo <owner/name>`, which renders the comment in Go and exits 5 when the PR changes a Harden now file.
4. Posts the output as a PR comment, updating its earlier comment (found by the `<!-- highstakes-triage -->` marker).
5. Fails the `triage` job when `pr check` exited 5.

The workflow also runs on `pull_request_review`, so an owner's approval ticks the checklist on the next run. The core step:

```yaml
      - name: Assess PR risk
        id: risk
        env:
          BASE: origin/${{ github.event.pull_request.base.ref }}
          APPROVALS: ${{ steps.approvals.outputs.logins }}
          REPO: ${{ github.repository }}
        run: |
          set +e
          highstakes pr check --base "$BASE" --format markdown --fail-on critical --approvals "$APPROVALS" --repo "$REPO" > pr-comment.md
          echo "code=$?" >> "$GITHUB_OUTPUT"
```

The comment is built to act on at a glance. It leads with the verdict (`⛔ Harden before merge`, `🛡 Protect before merge`, `🔧 Stabilize` or `✅ Routine`), the changed files per posture (`🛡 1 protect · 🔧 26 stabilize · ✅ 135 routine`) and one `🤖 AI may auto-merge: ✅ yes|❌ no (<reason>)` line from the [agent autonomy](cli-reference.md#agent-autonomy) levels. Then come **Do before merge (N of M done)**, a checklist of the Harden now and Protect files' items with an emoji per kind (👀 review, 🧪 tests, ✂️ split, 🚫 auto-merge off, 📝 docs, 👥 ownership; `[x]` for items the PR already satisfies: a test next to the risky file changed, a suggested owner approved), a short file table in plain words (posture emoji, File, If it breaks, Breaks often? as 🔴 with the strongest signal or 🟢 rarely, and Next step), a fold with one line per further file that needs action, **⚡ One-click actions** (prefilled GitHub issues for split, complexity, test and invariant work, a CODEOWNERS edit link, and the circuit breakers as things to do), a folded Mermaid quadrant chart of where the PR lands, and a folded **Why these actions?** block with the reasons, repo cutoffs, suggested reviewers, **Worth doing (does not block)** items and the backlog. `--repo "$REPO"` makes the issue and CODEOWNERS actions links; without a repo they stay plain text. See the [CLI reference](cli-reference.md#verdict-and-checklist).

### Secrets

Add `OPENROUTER_API_KEY` in **Settings > Secrets and variables > Actions > New repository secret**.

### Block Merge on Harden Now Files

The shipped workflow already gates with `--fail-on critical` (exit 5 on Harden now files). In **Settings > Branches > Branch protection rules**, add `triage` as a required status check. Use `--fail-on high` to block Protect files too, or `--shadow` to log decisions without failing while you calibrate.

## GitLab CI

```yaml
# .gitlab-ci.yml
heatmap:
  stage: test
  image: golang:1.25
  script:
    - go install github.com/zanetworker/highstakes/cmd/highstakes@latest
    - '[ -f .heatmap/config.yaml ] || highstakes init'
    - highstakes analyze
    # Markdown for a merge request note; exit 5 fails the job on Harden now files.
    - highstakes pr check --base origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME --format markdown --fail-on critical > risk.md
  variables:
    OPENROUTER_API_KEY: $OPENROUTER_API_KEY
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

## Generic CI

The CLI is the interface. Any CI that can run a binary works:

```sh
# Install
go install github.com/zanetworker/highstakes/cmd/highstakes@latest

# Analyze (cached, only re-assesses changed files)
export OPENROUTER_API_KEY="$YOUR_KEY"
[ -f .heatmap/config.yaml ] || highstakes init
highstakes analyze

# The PR comment body; exit 5 blocks Harden now files
highstakes pr check --base origin/main --format markdown --fail-on critical > comment.md

# Or read the JSON: verdict, checklist (each with done) and backlog
highstakes pr check --base origin/main --json > risk.json
jq -r '.verdict, (.checklist[] | "\(if .done then "[x]" else "[ ]" end) \(.text)")' risk.json
```

## Caching in CI

To avoid re-analyzing unchanged files on every PR, cache the `.heatmap/` directory:

```yaml
      - uses: actions/cache@v4
        with:
          path: .heatmap
          key: heatmap-${{ hashFiles('**/*.go', '**/*.py', '**/*.rs', '**/*.ts') }}
          restore-keys: heatmap-
```

This restores cached LLM assessments so only new or modified files are re-assessed.

## Cost in CI

With caching, CI costs are minimal. Only files changed in the PR are re-assessed. A typical PR touching 5-10 files costs $0.01-0.02 with the default model.

Without caching (first run or cache miss), a full analysis of a 500-file repo costs ~$0.15.
