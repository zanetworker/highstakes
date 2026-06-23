# CI Integration

Code Heatmap can run in any CI system to automatically assess PR risk and gate merges on high-risk files.

## GitHub Actions

### Quick Setup

```sh
highstakes github install
```

This creates `.github/workflows/heatmap-triage.yml`. Commit and push to activate.

### Manual Setup

Create `.github/workflows/heatmap-triage.yml`:

```yaml
name: Code Heatmap Triage
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: read

jobs:
  triage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Install heatmap
        run: go install github.com/zanetworker/highstakes/cmd/heatmap@latest

      - name: Analyze
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
        run: highstakes init && highstakes analyze

      - name: Check PR risk
        run: highstakes pr check --base origin/${{ github.base_ref }} --json > pr-risk.json

      - name: Post comment
        uses: actions/github-script@v7
        with:
          script: |
            const risk = JSON.parse(require('fs').readFileSync('pr-risk.json','utf8'));
            const e = {critical:'🔥🔥🔥',high:'🔥🔥',medium:'🔥',low:'🟢'};
            let body = '## '+e[risk.tier]+' Code Heatmap: '+risk.tier.toUpperCase()+'\n\n';
            body += '| File | Score | Tier | Lines |\n|------|-------|------|-------|\n';
            for (const f of risk.files_changed.sort((a,b)=>b.heat_score-a.heat_score))
              body += '| '+f.path+' | '+f.heat_score+' | '+e[f.tier]+' '+f.tier+' | +'+f.lines_added+'/-'+f.lines_deleted+' |\n';
            body += '\n**Review:** '+risk.review_requirements.min_reviewers+' reviewers';
            if (risk.review_requirements.requires_senior) body += ' (senior)';
            body += ', auto-merge: '+(risk.review_requirements.auto_merge?'✅':'❌')+'\n';
            const comments = await github.rest.issues.listComments({
              owner: context.repo.owner, repo: context.repo.repo, issue_number: context.issue.number});
            const existing = comments.data.find(c => c.body.includes('Code Heatmap:'));
            if (existing) await github.rest.issues.updateComment({
              owner: context.repo.owner, repo: context.repo.repo, comment_id: existing.id, body});
            else await github.rest.issues.createComment({
              owner: context.repo.owner, repo: context.repo.repo, issue_number: context.issue.number, body});
```

### Secrets

Add `OPENROUTER_API_KEY` in **Settings > Secrets and variables > Actions > New repository secret**.

### Block Merge on High-Risk PRs

Add a gate step after the PR check:

```yaml
      - name: Gate on risk tier
        run: |
          TIER=$(jq -r .tier pr-risk.json)
          if [ "$TIER" = "critical" ] || [ "$TIER" = "high" ]; then
            echo "::error::PR touches $TIER-tier files. Requires senior review."
            exit 1
          fi
```

Then in **Settings > Branches > Branch protection rules**, add `triage` as a required status check. PRs touching critical or high files cannot merge until a senior reviewer approves.

## GitLab CI

```yaml
# .gitlab-ci.yml
heatmap:
  stage: test
  image: golang:1.25
  script:
    - go install github.com/zanetworker/highstakes/cmd/heatmap@latest
    - highstakes init && highstakes analyze
    - highstakes pr check --base origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME --json > risk.json
    - |
      TIER=$(jq -r .tier risk.json)
      if [ "$TIER" = "critical" ] || [ "$TIER" = "high" ]; then
        echo "PR touches $TIER-tier files. Requires senior review."
        exit 1
      fi
  variables:
    OPENROUTER_API_KEY: $OPENROUTER_API_KEY
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

## Generic CI

The CLI is the interface. Any CI that can run a binary works:

```sh
# Install
go install github.com/zanetworker/highstakes/cmd/heatmap@latest

# Analyze (cached, only re-assesses changed files)
export OPENROUTER_API_KEY="$YOUR_KEY"
highstakes init && highstakes analyze

# Check PR risk
highstakes pr check --base origin/main --json > risk.json

# Read results
TIER=$(jq -r .tier risk.json)
SCORE=$(jq -r .heat_score risk.json)
FILES=$(jq -r '.files_changed | length' risk.json)
echo "PR risk: $TIER (score $SCORE), $FILES files changed"

# Gate
if [ "$TIER" = "critical" ] || [ "$TIER" = "high" ]; then
  echo "Requires human review"
  exit 1
fi
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
