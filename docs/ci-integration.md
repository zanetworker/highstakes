# CI Integration

Code Heatmap can run in any CI system to automatically assess PR risk and gate merges on high-risk files.

## GitHub Actions

### Setup

Copy [`.github/workflows/heatmap-triage.yml`](https://github.com/zanetworker/highstakes/blob/main/.github/workflows/heatmap-triage.yml) into your repo. Each run:

1. Runs `highstakes init` only when `.heatmap/config.yaml` is missing (a committed config is kept), then `highstakes analyze` (cached by file content).
2. Collects the reviewers whose latest review is an approval, with `gh api repos/<repo>/pulls/<n>/reviews`.
3. Runs `highstakes pr check --format markdown --fail-on critical --approvals <logins>`, which renders the comment in Go and exits 5 when the PR changes a Harden now file.
4. Posts the output as a PR comment, updating its earlier comment (found by the `<!-- highstakes-triage -->` marker).
5. Fails the `triage` job when `pr check` exited 5.

The workflow also runs on `pull_request_review`, so an owner's approval ticks the checklist on the next run. The core step:

```yaml
      - name: Assess PR risk
        id: risk
        env:
          BASE: origin/${{ github.event.pull_request.base.ref }}
          APPROVALS: ${{ steps.approvals.outputs.logins }}
        run: |
          set +e
          highstakes pr check --base "$BASE" --format markdown --fail-on critical --approvals "$APPROVALS" > pr-comment.md
          echo "code=$?" >> "$GITHUB_OUTPUT"
```

The comment leads with the verdict (`⛔ Harden before merge`, `🛡 Protect`, `🔧 Stabilize` or `✅ Routine`) and one sentence naming the riskiest file, then one `AI may auto-merge: yes|no (<reason>)` line from the [agent autonomy](cli-reference.md#agent-autonomy) levels, **Do before merge** as a checklist of the Harden now and Protect files' items (`[x]` for items the PR already satisfies: a test next to the risky file changed, a suggested owner approved), a short file table in plain words (File, If it breaks, Breaks often?, Do; scores and cutoffs are in the folded details), and a folded **Why these actions?** block with the reasons, repo cutoffs, suggested reviewers, **Worth doing (does not block)** items (Stabilize work such as refactoring the touched function) and the backlog (plain items with their commands, such as a CODEOWNERS line to add; no checkboxes). See the [CLI reference](cli-reference.md#verdict-and-checklist).

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
