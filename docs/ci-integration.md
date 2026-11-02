# CI Integration

Code Heatmap can run in any CI system to automatically assess PR risk and gate merges on high-risk files.

## GitHub Actions

### Setup

Copy [`.github/workflows/heatmap-triage.yml`](https://github.com/zanetworker/highstakes/blob/main/.github/workflows/heatmap-triage.yml) into your repo. Each run:

1. Runs `highstakes init` only when `.heatmap/config.yaml` is missing (a committed config is kept), then `highstakes analyze` (cached by file content).
2. Verifies the sign-off label with `highstakes pr signoff` and reads the PR's live state with `highstakes pr state` (auto-merge, the approvals of the head commit, and which approvers have write access). Both run before the analysis.
3. Runs `highstakes pr check --format markdown --fail-on critical --approvals <logins> --maintainer-approvals <logins> --repo <owner/name> --auto-merge-enabled <true|false> [--signed-off]`, which renders the comment in Go and exits 5 when the PR changes a Harden now file and **Do before merge** items are still open.
4. Checks with `highstakes pr fresh` that the PR did not change during the run, then posts the output as a PR comment, updating its earlier comment (found by the `<!-- highstakes-triage -->` marker).
5. Checks freshness again and fails the `triage` job when `pr check` exited 5.

All the sign-off and freshness logic lives in those Go commands (tested in `internal/prstate`); the workflow only passes them the event's values. Copy it from `highstakes github install` rather than writing your own checks.

The workflow also runs on `pull_request_review`, so an approval of the current head commit ticks the checklist on the next run (an approval of an earlier commit does not), on `labeled`/`unlabeled`, so the sign-off label does too, and on `auto_merge_enabled`/`auto_merge_disabled`. A suggested owner's approval ticks that file's review item; any approval from someone with write access ticks Protect files' owner-review items, never a Harden now senior sign-off. The auto-merge state is read live from the API, not from the event payload, since it may have changed since the run was queued. `SIGNED_OFF` must come from `pr signoff`: passing `--signed-off` because the label is present would skip the write-access check and the commit binding. The core steps (the job sets `GH_TOKEN`, `REPO`, `PR` and `HEAD_SHA` from the event):

```yaml
      - name: Verify sign-off
        id: signoff
        env:
          ACTION: ${{ github.event.action }}
          LABEL: ${{ github.event.label.name }}
          PUSHED_AT: ${{ github.event.pull_request.updated_at }}
        run: highstakes pr signoff --repo "$REPO" --pr "$PR" --head-sha "$HEAD_SHA" --action "$ACTION" --label "$LABEL" --pushed-at "$PUSHED_AT" >> "$GITHUB_OUTPUT"

      - name: Read PR state
        id: state
        run: highstakes pr state --repo "$REPO" --pr "$PR" --head-sha "$HEAD_SHA" >> "$GITHUB_OUTPUT"

      # ... analyze ...

      - name: Assess PR risk
        id: risk
        env:
          BASE: origin/${{ github.event.pull_request.base.ref }}
          SIGNED_OFF: ${{ steps.signoff.outputs.signed }} # verified by pr signoff: never the label alone
          APPROVALS: ${{ steps.state.outputs.approvals }}
          MAINTAINERS: ${{ steps.state.outputs.maintainers }}
          AUTO_MERGE: ${{ steps.state.outputs.auto_merge }}
        run: |
          set +e
          signed=
          if [ "$SIGNED_OFF" = "true" ]; then signed=--signed-off; fi
          highstakes pr check --base "$BASE" --format markdown --fail-on critical --approvals "$APPROVALS" --maintainer-approvals "$MAINTAINERS" --repo "$REPO" --auto-merge-enabled "$AUTO_MERGE" $signed > pr-comment.md
          echo "code=$?" >> "$GITHUB_OUTPUT"
```

### Sign-off label

Review items ask for an owner's approval, but GitHub does not let authors approve their own PRs, so a sole maintainer could never tick them. A maintainer with write access reviews the change and adds the label `highstakes:signed-off` to the PR; the run that the labeling triggers ticks the sign-off and owner-review items ("signed off with the highstakes:signed-off label"). Create the label once in **Issues > Labels**. The label is a sign-off for one commit, not a standing pass:

- **Who:** only the run triggered by adding the label can sign, and only when the person who added it has write, maintain or admin access (triage collaborators can add labels too, so it checks).
- **Which commit:** that run records a `highstakes/signed-off/pr-<number>` success status on the PR's head at the moment of labeling (scoped to the PR, so another PR on the same commit cannot borrow it). The status names the label event it was minted for (who added the label, and when), and every run trusts the label only when the current head carries that status from the workflow's own bot (`github-actions[bot]`) for the latest addition, and the person behind it still has write access. A sign-off from an earlier addition never stands in for a newer one. After a push the new head has no such status, so it is unsigned whatever order the runs finish in; commenters cannot forge the status.
- **Withdrawal:** GitHub's label history decides whether the label still stands (the latest add or remove wins). Removing it reopens the items, and the workflow records a newer failing status; only the latest status for the head counts.
- **New pushes:** a push also removes the label, so new commits need a fresh review and the label added again. The sign-off check runs before the analysis, so a failed analysis cannot leave a stale label in place.
- **Auto-merge:** the run reads the PR's auto-merge state live from the API, not from the event that queued it, and reads it again before the gate passes; if it changed during the run, the gate fails and the run triggered by the change decides. The same goes for the sign-off label: before posting the comment the run checks the head, the auto-merge state and the latest label change again, and before passing it checks the auto-merge state, the latest label change and the approvals again; a run that saw an older state leaves the comment or the decision to the newer run (a run for an older head only affects that commit's check). GitHub can still merge between the last run and the merge itself, and a workflow cannot block that, so to really forbid auto-merge on these PRs, turn off **Allow auto-merge** in the repository settings or require a review in branch protection.

This needs `issues: write` and `statuses: write` in the workflow permissions.

The comment is built to act on at a glance. It leads with the verdict (`⛔ Harden before merge`, `🛡 Protect before merge`, `🔧 Stabilize` or `✅ Routine`), the changed files per posture (`🛡 1 protect · 🔧 26 stabilize · ✅ 135 routine`) and one `🤖 AI may auto-merge: ✅ yes|❌ no (<reason>)` line from the [agent autonomy](cli-reference.md#agent-autonomy) levels. Then come **Do before merge (N of M done)**, a checklist of the Harden now and Protect files' items with an emoji per kind (👀 review, 🧪 tests, ✂️ split, 🚫 auto-merge off, 📝 docs, 👥 ownership; `[x]` for items the PR already satisfies: a test next to the risky file changed, a suggested owner approved, the sign-off label is on, auto-merge is off) with the gate's state under the heading (`⛔ Merge blocked: 2 before-merge items open` or `✅ All before-merge items done: ready to merge`), a short file table in plain words (posture emoji, File, If it breaks, Breaks often? as 🔴 with the strongest signal or 🟢 rarely, and Next step), a fold that groups further files by next step, **⚡ One-click actions** (a smaller-PR ask once for the whole PR; prefilled GitHub issues for split, complexity, test and invariant work, each a full brief with why it matters, why now, a suggested approach, a done-when checklist and context, previewed in a folded **What's in this issue** under the link; a CODEOWNERS edit link; and the circuit breakers as things to do), a folded Mermaid quadrant chart of where the PR lands (numbered points with a legend), and a folded **Why these actions?** block with the reasons, repo cutoffs, suggested reviewers, **Worth doing (does not block)** items and the backlog. `--repo "$REPO"` makes the issue and CODEOWNERS actions links; without a repo they stay plain text. The issues link back to the PR, whose number `pr check` reads from `$GITHUB_REF` in Actions (pass `--pr N` elsewhere). See the [CLI reference](cli-reference.md#verdict-and-checklist).

### Secrets

Add `OPENROUTER_API_KEY` in **Settings > Secrets and variables > Actions > New repository secret**.

### Block Merge on Harden Now Files

The shipped workflow already gates with `--fail-on critical`: exit 5 while a PR that changes Harden now files has open **Do before merge** items. Working through the checklist (tests next to the file, sign-off by approval or label, auto-merge off) unblocks it. In **Settings > Branches > Branch protection rules**, add `triage` as a required status check. Use `--fail-on high` to gate Protect files too, `--strict` to block on the tier alone whatever is done, or `--shadow` to log decisions without failing while you calibrate.

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
    # Markdown for a merge request note; exit 5 fails the job while a Harden now file has open before-merge items.
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

# The PR comment body; exit 5 while a Harden now file has open before-merge items
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
