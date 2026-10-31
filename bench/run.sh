#!/usr/bin/env bash
# Multi-language calibration benchmark for HighStakes.
#
# Clones each repo in bench/repos.yaml (full history) into a cache dir, checks
# out the pinned commit, runs `highstakes analyze` and `highstakes check
# calibration` (in-sample and holdout), and writes a combined table to
# bench/results/<date>.md and .json. See bench/README.md.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bench/run.sh [--llm [--model ID]] [--include-disabled] [--bin PATH] [--cache-dir DIR] [--out-dir DIR]

  --llm            Run analyze with the LLM (paid; needs OPENROUTER_API_KEY or
                   HIGHSTAKES_API_KEY). Default is --no-llm.
  --model ID       OpenRouter model for --llm (default: the CLI default).
  --include-disabled
                   Also run repos marked `enabled: false` in repos.yaml.
  --bin PATH       highstakes binary to use (default: build from this tree into
                   the cache dir).
  --cache-dir DIR  Where clones live (default: $HIGHSTAKES_BENCH_CACHE, else
                   the user cache dir + /highstakes-bench). Must be outside
                   this repo.
  --out-dir DIR    Where results go (default: bench/results).
EOF
}

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$BENCH_DIR/.." && pwd -P)"

default_cache() {
  if [[ -n "${HIGHSTAKES_BENCH_CACHE:-}" ]]; then echo "$HIGHSTAKES_BENCH_CACHE"
  elif [[ -n "${XDG_CACHE_HOME:-}" ]]; then echo "$XDG_CACHE_HOME/highstakes-bench"
  elif [[ "$(uname -s)" == "Darwin" ]]; then echo "$HOME/Library/Caches/highstakes-bench"
  else echo "$HOME/.cache/highstakes-bench"
  fi
}

LLM=0
INCLUDE_DISABLED=0
BIN=""
MODEL=""
CACHE_DIR="$(default_cache)"
OUT_DIR="$BENCH_DIR/results"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --llm) LLM=1; shift ;;
    --model) MODEL="${2:?--model needs an OpenRouter model ID}"; shift 2 ;;
    --include-disabled) INCLUDE_DISABLED=1; shift ;;
    --bin) BIN="${2:?--bin needs a path}"; shift 2 ;;
    --cache-dir) CACHE_DIR="${2:?--cache-dir needs a path}"; shift 2 ;;
    --out-dir) OUT_DIR="${2:?--out-dir needs a path}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1 (valid: --llm, --model, --include-disabled, --bin, --cache-dir, --out-dir, --help)" >&2; exit 2 ;;
  esac
done

if [[ -n "$MODEL" && $LLM -eq 0 ]]; then
  echo "--model only applies with --llm" >&2; exit 2
fi

for tool in git jq awk; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 3; }
done

mkdir -p "$CACHE_DIR" "$OUT_DIR"
CACHE_DIR="$(cd "$CACHE_DIR" && pwd -P)"
OUT_DIR="$(cd "$OUT_DIR" && pwd -P)"
case "$CACHE_DIR/" in
  "$REPO_ROOT"/*) rmdir "$CACHE_DIR" 2>/dev/null || true
    echo "cache dir $CACHE_DIR is inside the highstakes repo; pick a dir outside it" >&2; exit 2 ;;
esac

if [[ $LLM -eq 1 && -z "${OPENROUTER_API_KEY:-}${HIGHSTAKES_API_KEY:-}" ]]; then
  echo "--llm needs OPENROUTER_API_KEY or HIGHSTAKES_API_KEY" >&2; exit 2
fi

if [[ -z "$BIN" ]]; then
  BIN="$CACHE_DIR/bin/highstakes"
  echo "Building highstakes from $REPO_ROOT"
  (cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/highstakes)
fi
BIN="$(cd "$(dirname "$BIN")" && pwd -P)/$(basename "$BIN")"
[[ -x "$BIN" ]] || { echo "not executable: $BIN" >&2; exit 2; }

# repos.yaml -> TSV: name url language commit tier mirror holdout_days.
# Empty optional fields become "-" because read collapses adjacent tabs.
# Disabled entries are dropped unless --include-disabled.
REPOS_TSV="$(awk -v all="$INCLUDE_DISABLED" '
  function val() { sub(/^[^:]*:[[:space:]]*/, ""); sub(/[[:space:]]+#.*$/, ""); return $0 }
  function flush() {
    if (name != "" && (enabled != "false" || all == 1))
      print name "\t" url "\t" lang "\t" sha "\t" (tier == "" ? "general" : tier) "\t" (mirror == "" ? "-" : mirror) "\t" (hd == "" ? 180 : hd)
    name = url = lang = sha = tier = mirror = hd = enabled = ""
  }
  /^[[:space:]]*- name:/         { flush(); sub(/- /, ""); name = val(); next }
  /^[[:space:]]+url:/            { url = val(); next }
  /^[[:space:]]+language:/       { lang = val(); next }
  /^[[:space:]]+commit:/         { sha = val(); next }
  /^[[:space:]]+tier:/           { tier = val(); next }
  /^[[:space:]]+local_mirror:/   { mirror = val(); next }
  /^[[:space:]]+holdout_days:/   { hd = val(); next }
  /^[[:space:]]+enabled:/        { enabled = val(); next }
  END { flush() }
' "$BENCH_DIR/repos.yaml")"
[[ -n "$REPOS_TSV" ]] || { echo "no enabled repos in $BENCH_DIR/repos.yaml" >&2; exit 2; }

utc_date() { # epoch seconds -> YYYY-MM-DD (UTC), GNU or BSD date
  date -u -d "@$1" +%Y-%m-%d 2>/dev/null || date -u -r "$1" +%Y-%m-%d
}

# Approximates the analyzer's file walk (extensions and default excludes).
estimate_files() {
  git -C "$1" ls-files \
    | grep -E '\.(go|ts|tsx|js|jsx|py|java|rb|rs|cpp|cc|cxx|c|cs|ya?ml|toml|json|tf)$|(^|/)Dockerfile(\.[^/]*)?$' \
    | awk -F/ '{
        for (i = 1; i < NF; i++) {
          d = $i
          if ((d ~ /^\./ && d != ".github") || d ~ /^(node_modules|vendor|__pycache__|venv|dist|build|target|site-packages|third_party|third-party|external|deps)$/ || d ~ /^(distribution-|env)/ || d ~ /\.egg-info$/) next
        }
        n++
      } END { print n + 0 }'
}

# Phase 1: clone (full history), pin, estimate.
echo "Cache dir: $CACHE_DIR"
TOTAL=0
while IFS=$'\t' read -r name url lang sha tier mirror holdout_days; do
  dir="$CACHE_DIR/repos/$name"
  mirror="${mirror/#\~/$HOME}"
  [[ "$mirror" != "-" && -d "$mirror/.git" ]] || mirror=""
  # A local mirror is only read from (clone/fetch); the clone's origin is the URL.
  if [[ ! -d "$dir/.git" ]]; then
    if [[ -n "$mirror" ]]; then
      echo "Cloning $name from local mirror $mirror (full history)"
      git clone --quiet --no-hardlinks "$mirror" "$dir"
      git -C "$dir" remote set-url origin "$url"
    else
      echo "Cloning $name (full history)"
      git clone --quiet "$url" "$dir"
    fi
  fi
  if [[ "$(git -C "$dir" rev-parse --is-shallow-repository)" == "true" ]]; then
    git -C "$dir" fetch --quiet --unshallow origin
  fi
  if ! git -C "$dir" cat-file -e "$sha^{commit}" 2>/dev/null && [[ -n "$mirror" ]]; then
    # The pin is often only on the mirror's remote-tracking refs, which a clone skips.
    git -C "$dir" fetch --quiet "$mirror" '+refs/remotes/*:refs/mirror/*' || true
  fi
  if ! git -C "$dir" cat-file -e "$sha^{commit}" 2>/dev/null; then
    git -C "$dir" fetch --quiet origin
  fi
  git -C "$dir" -c advice.detachedHead=false checkout --quiet --force "$sha"
  n="$(estimate_files "$dir")"
  TOTAL=$((TOTAL + n))
  printf '  %-20s %-8s %-11s ~%s files\n' "$name" "$tier" "$lang" "$n"
done <<< "$REPOS_TSV"
echo "Estimated files to analyze: ~$TOTAL"
if [[ $LLM -eq 1 ]]; then
  echo "LLM mode: up to ~$TOTAL paid assessments (cached files are skipped)."
  if [[ -t 0 ]]; then
    read -r -p "Continue? [y/N] " ok
    [[ "$ok" == y || "$ok" == Y ]] || { echo "aborted"; exit 0; }
  fi
fi

# Phase 2: analyze and calibrate.
# Windows are anchored to the pinned commit date, not today, so the events
# counted stay the same when the benchmark is rerun later.
RAW_DIR="$CACHE_DIR/raw"
mkdir -p "$RAW_DIR"
SUMMARIES="$RAW_DIR/summaries.jsonl"
: > "$SUMMARIES"
NOW="$(date +%s)"
while IFS=$'\t' read -r name url lang sha tier _mirror holdout_days; do
  dir="$CACHE_DIR/repos/$name"
  commit_ts="$(git -C "$dir" log -1 --format=%ct "$sha")"
  since_days=$(( (NOW - (commit_ts - 365*86400) + 86399) / 86400 ))
  holdout="$(utc_date $((commit_ts - holdout_days*86400)))"
  est="$(estimate_files "$dir")"

  echo "== $name ($tier, $lang) @ ${sha:0:12}"
  start="$(date +%s)"
  analyze_flags=(--json)
  [[ $LLM -eq 1 ]] || analyze_flags+=(--no-llm)
  [[ -n "$MODEL" ]] && analyze_flags+=(--model "$MODEL")
  mkdir -p "$dir/.heatmap"
  if ! (cd "$dir" && "$BIN" analyze "${analyze_flags[@]}") > "$RAW_DIR/$name-analyze.json" 2> "$RAW_DIR/$name-analyze.log"; then
    echo "analyze failed for $name; last lines of $RAW_DIR/$name-analyze.log:" >&2
    tail -5 "$RAW_DIR/$name-analyze.log" >&2
    exit 1
  fi
  (cd "$dir" && "$BIN" check calibration --since "${since_days}d" --json) > "$RAW_DIR/$name-insample.json"
  (cd "$dir" && "$BIN" check calibration --holdout "$holdout" --json) > "$RAW_DIR/$name-holdout.json"
  wall=$(( $(date +%s) - start ))
  echo "   done in ${wall}s"

  jq -n -c \
    --arg name "$name" --arg url "$url" --arg lang "$lang" --arg sha "$sha" --arg tier "$tier" \
    --arg commit_date "$(utc_date "$commit_ts")" --arg holdout "$holdout" --arg holdout_days "$holdout_days" \
    --argjson est "$est" --argjson wall "$wall" --argjson llm "$LLM" \
    --slurpfile ins "$RAW_DIR/$name-insample.json" \
    --slurpfile hold "$RAW_DIR/$name-holdout.json" '
    def metrics: {
      mode, from, to, k, files, files_with_events, base_rate,
      events: .events.total,
      signals: (.signals | map({key: .name, value: {
        available, spearman, precision_at_k, recall_at_k, lift, note}}) | from_entries)
    };
    {name: $name, tier: $tier, url: $url, language: $lang, commit: $sha, commit_date: $commit_date,
     llm: ($llm == 1), estimated_files: $est, wall_seconds: $wall,
     holdout_cutoff: $holdout, holdout_days: ($holdout_days | tonumber),
     in_sample: ($ins[0] | metrics), holdout: ($hold[0] | metrics)}' >> "$SUMMARIES"
done <<< "$REPOS_TSV"

# Phase 3: combined results.
STAMP="$(date +%Y-%m-%d)"
OUT_JSON="$OUT_DIR/$STAMP.json"
OUT_MD="$OUT_DIR/$STAMP.md"
jq -s --arg generated "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --argjson llm "$LLM" \
  --arg version "$("$BIN" --version 2>/dev/null || echo unknown)" \
  'def rank: {primary: 0, general: 1}[.tier] // 2;
   {generated_at: $generated, llm: ($llm == 1), highstakes: $version, repos: (sort_by(rank))}' \
  "$SUMMARIES" > "$OUT_JSON"

jq -r '
  def fixed($scale): (. * $scale | round) as $i | ($i | if . < 0 then -. else . end) as $a
    | (if $i < 0 then "-" else "" end) + (($a / $scale | floor) | tostring) + "."
      + (($a % $scale) | tostring | if length < (($scale | tostring | length) - 1) then "0" + . else . end);
  def n2: if . == null then "n/a" else fixed(100) end;
  def pct: if . == null then "n/a" else (. * 100 | fixed(10)) + "%" end;
  def sig($s): if $s.available then $s else {spearman: null, precision_at_k: null, lift: null} end;
  def rho($m; $k): sig($m.signals[$k]).spearman | n2;
  def val($m; $k; $f): sig($m.signals[$k])[$f];
  def mean: map(select(. != null)) | if length == 0 then null else add / length end;
  .repos as $repos
  | ([$repos[].tier] | unique | sort_by({primary: 0, general: 1}[.] // 2)) as $tiers
  |
  "# HighStakes calibration benchmark (\(.generated_at[0:10]))",
  "",
  "Mode: \(if .llm then "LLM" else "no LLM (blast radius not assessed)" end). Binary: `\(.highstakes)`. K = 10.",
  "Event windows are anchored to each pinned commit: in-sample counts events in the 365 days before it, holdout scores with history up to the cutoff (commit date minus the repo holdout days, default 180) and counts events after it. Primary repos are the use cases HighStakes is tuned for; general repos guard against overfitting to them.",
  "",
  "## Heat score",
  "",
  "| Repo | Tier | Language | Files | Wall (s) | Mode | Base rate | Heat Spearman | Precision@10 | Lift |",
  "|---|---|---|---:|---:|---|---:|---:|---:|---:|",
  ($repos[] as $r | ($r.in_sample, $r.holdout) as $m | sig($m.signals.heat) as $h |
    "| \($r.name) | \($r.tier) | \($r.language) | \($m.files) | \($r.wall_seconds) | \($m.mode) | \($m.base_rate | pct) | \($h.spearman | n2) | \($h.precision_at_k | n2) | \($h.lift | n2) |"),
  "",
  "## Averages by tier",
  "",
  "Means over repos in the tier; `n/a` values are left out of a mean.",
  "",
  "| Tier | Mode | Repos | Heat Spearman | Precision@10 | Lift | Churn | Complexity | Centrality | Blast radius |",
  "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|",
  ($tiers[] as $t | ("in_sample", "holdout") as $mode | [$repos[] | select(.tier == $t) | .[$mode]] as $ms |
    "| \($t) | \($ms[0].mode) | \($ms | length) | \([$ms[] | val(.; "heat"; "spearman")] | mean | n2) | \([$ms[] | val(.; "heat"; "precision_at_k")] | mean | n2) | \([$ms[] | val(.; "heat"; "lift")] | mean | n2) | \([$ms[] | val(.; "churn"; "spearman")] | mean | n2) | \([$ms[] | val(.; "complexity"; "spearman")] | mean | n2) | \([$ms[] | val(.; "centrality"; "spearman")] | mean | n2) | \([$ms[] | val(.; "blast_radius"; "spearman")] | mean | n2) |"),
  "",
  "## Per-factor Spearman (vs event count)",
  "",
  "| Repo | Tier | Language | Mode | Heat | Churn | Complexity | Centrality | Blast radius |",
  "|---|---|---|---|---:|---:|---:|---:|---:|",
  ($repos[] as $r | ($r.in_sample, $r.holdout) as $m |
    "| \($r.name) | \($r.tier) | \($r.language) | \($m.mode) | \(rho($m; "heat")) | \(rho($m; "churn")) | \(rho($m; "complexity")) | \(rho($m; "centrality")) | \(rho($m; "blast_radius")) |"),
  "",
  "## Repos",
  "",
  "| Repo | Commit | Commit date | Holdout cutoff |",
  "|---|---|---|---|",
  ($repos[] | "| [\(.name)](\(.url | sub("\\.git$"; ""))) | `\(.commit[0:12])` | \(.commit_date) | \(.holdout_cutoff) |"),
  "",
  "Fix history measures how likely code is to break, not the blast radius if it does. See bench/README.md."
' "$OUT_JSON" > "$OUT_MD"

echo "Wrote $OUT_MD"
echo "Wrote $OUT_JSON"
