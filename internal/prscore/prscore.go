package prscore

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

// CalculatePRRisk assesses the risk of a set of changed files
func CalculatePRRisk(prNumber int, changes []FileChange, hm *types.Heatmap, thresholds types.TierThresholds) types.PRRisk {
	risk := types.PRRisk{
		PRNumber:     prNumber,
		FilesChanged: make([]types.FileChange, 0, len(changes)),
	}

	maxHeat, importance := 0, 0
	maxTier := types.TierLow
	var unscored []string

	for _, change := range changes {
		heat, ok := hm.Files[change.Path]

		fc := types.FileChange{
			Path:         change.Path,
			LinesAdded:   change.LinesAdded,
			LinesDeleted: change.LinesDeleted,
			IsNew:        change.IsNew,
		}

		switch {
		case ok:
			fc.HeatScore = heat.HeatScore
			fc.Tier = heat.Tier
			fc.Likelihood, fc.Impact, fc.ImpactAssessed = heat.Likelihood, heat.Impact, heat.ImpactAssessed
			fc.Quadrant, fc.QuadrantLabel = heat.Quadrant, heat.Quadrant.Label()
		case isNonCode(change.Path):
			fc.HeatScore = 0
			fc.Tier = types.TierLow
		default:
			// Unknown files must not silently pass: inherit the nearest scored
			// directory's heat and tier (the tier may come from a quadrant).
			fc.HeatScore, fc.Tier = inheritedHeat(change.Path, hm, thresholds)
			fc.Unscored = true
			unscored = append(unscored, change.Path)
		}

		risk.FilesChanged = append(risk.FilesChanged, fc)

		if fc.HeatScore > maxHeat {
			maxHeat = fc.HeatScore
		}
		if tierRank[fc.Tier] > tierRank[maxTier] {
			maxTier = fc.Tier
		}
		imp := fc.HeatScore
		if ok && heat.ImpactAssessed {
			imp = heat.Impact
		}
		importance = max(importance, imp)
	}

	risk.HeatScore = maxHeat
	// Importance is what breaks if the touched code breaks: the max impact of
	// assessed files, falling back to heat for files without an assessment.
	risk.Importance = importance
	risk.ChangeRisk, risk.ChangeRiskFactors = ChangeRisk(changes, hm)
	risk.ChangeRiskLevel = ChangeRiskLevel(risk.ChangeRisk)
	// The PR takes the most severe file tier, so quadrant tiers carry over.
	risk.Tier = maxTier
	risk.Verdict = types.TierPosture(maxTier)
	risk.LikelyCutoff, risk.ImpactThreshold = hm.Metadata.LikelyCutoff, hm.Metadata.ImpactThreshold
	risk.ReviewRequirements = scorer.CalculateReviewRequirements(risk.Tier)
	risk.CircuitBreakerSignals = checkCircuitBreakers(changes, hm)
	if len(unscored) > 0 {
		risk.CircuitBreakerSignals = append(risk.CircuitBreakerSignals,
			"Unscored new file(s), heat inherited from directory: "+strings.Join(unscored, ", "))
	}

	return risk
}

// FileChange represents a file diff in a PR
type FileChange struct {
	Path         string
	LinesAdded   int
	LinesDeleted int
	IsNew        bool
	Deleted      bool // the PR deletes the file (a rename counts as delete plus add)
}

// checkCircuitBreakers returns warning signals for the PR
func checkCircuitBreakers(changes []FileChange, hm *types.Heatmap) []string {
	var signals []string

	// Large diff
	totalLines := 0
	for _, c := range changes {
		totalLines += c.LinesAdded + c.LinesDeleted
	}
	if totalLines > 500 {
		signals = append(signals, "Large diff (>500 lines)")
	}

	// Many languages
	langs := map[string]bool{}
	for _, c := range changes {
		if heat, ok := hm.Files[c.Path]; ok && heat.Language != "" {
			langs[heat.Language] = true
		}
	}
	if len(langs) > 3 {
		signals = append(signals, "Many languages (>3)")
	}

	// Low test ratio
	testLines := 0
	codeLines := 0
	for _, c := range changes {
		if isTestFile(c.Path) {
			testLines += c.LinesAdded
		} else {
			codeLines += c.LinesAdded
		}
	}
	if codeLines > 0 {
		ratio := float64(testLines) / float64(codeLines)
		if ratio < 0.3 {
			signals = append(signals, "Low test ratio (<0.3)")
		}
	}

	return signals
}

// inheritedHeat returns the max heat and most severe tier of scored files in
// the file's directory, walking up to the nearest ancestor directory that has
// any. Returns 0 and TierLow if none. A file with assessed impact keeps its
// quadrant tier; any other file is tiered from its heat with thresholds, so
// the repo's current config applies.
func inheritedHeat(path string, hm *types.Heatmap, thresholds types.TierThresholds) (int, types.Tier) {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		maxHeat, maxTier, found := 0, types.TierLow, false
		for p, heat := range hm.Files {
			if filepath.Dir(p) != dir {
				continue
			}
			found = true
			maxHeat = max(maxHeat, heat.HeatScore)
			tier := heat.Tier
			if !heat.ImpactAssessed {
				tier = scorer.CalculateTier(heat.HeatScore, thresholds)
			}
			if tierRank[tier] > tierRank[maxTier] {
				maxTier = tier
			}
		}
		if found || dir == "." || dir == "/" {
			return maxHeat, maxTier
		}
	}
}

// isDocFile reports whether a path is documentation, which carries no runtime risk.
// Dependency manifests such as requirements.txt are not documentation.
func isDocFile(path string) bool {
	if isDependencyManifest(filepath.Base(path)) {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx", ".rst", ".txt":
		return true
	}
	return false
}

// inertExts are images, fonts and media. Executables, libraries, archives
// and other compiled artifacts are not inert: they run, so they inherit
// directory risk like any unscored file.
var inertExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".webp": true,
	".pdf": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".mp4": true, ".mov": true,
}

// ciDirs are top-level dot directories that hold CI, which stays scored.
var ciDirs = map[string]bool{".github": true, ".gitlab": true, ".circleci": true}

// configExts are data formats tools read as configuration; scripts never are.
var configExts = map[string]bool{
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".cfg": true, ".conf": true,
}

// configDotfiles are extensionless tool dotfiles that hold data, not code.
var configDotfiles = map[string]bool{
	".gitignore": true, ".gitattributes": true, ".dockerignore": true, ".editorconfig": true, ".mailmap": true,
	".prettierrc": true, ".prettierignore": true, ".eslintignore": true, ".npmrc": true, ".nvmrc": true,
	".python-version": true, ".tool-versions": true,
}

// isNonCode reports files a PR may add without them being "unscored new
// files": inert files and config files (see isInert, isConfigFile).
func isNonCode(p string) bool { return isInert(p) || isConfigFile(p) }

// isInert reports files with no runtime effect: docs, images, fonts, media,
// golden files and testdata.
func isInert(p string) bool {
	p = filepath.ToSlash(p)
	base := filepath.Base(p)
	inTestdata := strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/")
	return isDocFile(p) || inertExts[strings.ToLower(filepath.Ext(base))] || strings.HasSuffix(base, ".golden") ||
		(inTestdata && isFixtureData(base))
}

// isFixtureData reports data formats a test reads but does not run. It is an
// allowlist: test harnesses run or load anything else (scripts, sources,
// libraries, archives), so unknown fixture formats are never inert.
func isFixtureData(base string) bool {
	switch strings.ToLower(filepath.Ext(base)) {
	case ".json", ".yaml", ".yml", ".toml", ".csv", ".tsv", ".txt", ".xml", ".golden", ".md",
		".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// isConfigFile reports dependency manifests (still config changes in
// ChangeRisk), lock files (go.sum too) and tool config under a leading-dot
// path such as .golangci.yml or .impeccable/config.json: data formats
// (configExts) or known dotfiles only, so hooks and scripts stay scored.
// CI directories are not included.
func isConfigFile(p string) bool {
	p = filepath.ToSlash(p)
	base := filepath.Base(p)
	if isDependencyManifest(base) || base == "go.sum" || strings.HasSuffix(base, ".lock") {
		return true
	}
	first := strings.SplitN(p, "/", 2)[0]
	if !strings.HasPrefix(first, ".") || ciDirs[first] || first == ".gitlab-ci.yml" {
		return false
	}
	return configDotfiles[base] || configExts[strings.ToLower(filepath.Ext(base))]
}

func isTestFile(path string) bool {
	base := filepath.Base(path)

	suffixes := []string{"_test.go", ".test.ts", ".test.js", ".spec.ts", ".spec.js", "_test.py"}
	for _, s := range suffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}

	prefixes := []string{"test_"}
	for _, p := range prefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}

	return false
}

// Decisions reported by pr check when a --fail-on gate is set.
const (
	DecisionPass  = "pass"
	DecisionBlock = "block"
)

// ReasonAllDone is the decision reason when the PR is at or above the gate
// and every before-merge item is done.
const ReasonAllDone = "all before-merge items done"

var tierRank = map[types.Tier]int{types.TierLow: 1, types.TierMedium: 2, types.TierHigh: 3, types.TierCritical: 4}

// Decide gates a PR at failOn: a tier below it passes; at or above it the
// PR blocks while open before-merge items remain, and passes once they are
// done. strict blocks on the tier alone. reason says why; an empty failOn
// never blocks and gives no reason.
func Decide(tier, failOn types.Tier, open int, strict bool) (decision, reason string) {
	switch {
	case failOn == "":
		return DecisionPass, ""
	case tierRank[tier] < tierRank[failOn]:
		return DecisionPass, fmt.Sprintf("tier %s is below --fail-on %s", tier, failOn)
	case strict:
		return DecisionBlock, fmt.Sprintf("tier %s is at or above --fail-on %s (--strict)", tier, failOn)
	case open == 1:
		return DecisionBlock, "1 before-merge item open"
	case open > 1:
		return DecisionBlock, fmt.Sprintf("%d before-merge items open", open)
	}
	return DecisionPass, ReasonAllDone
}

// OpenItems counts the checklist items still open. The smaller-PR ask is
// left out: the comment raises it once as a one-click action, not as an
// item anyone can tick.
func OpenItems(checklist []types.Action) int {
	n := 0
	for _, a := range checklist {
		if !a.Done && a.ID != "smaller-pr" {
			n++
		}
	}
	return n
}

// ChangeRiskLevel buckets a 0-100 change risk score.
func ChangeRiskLevel(score int) string {
	switch {
	case score >= 60:
		return "high"
	case score >= 30:
		return "medium"
	}
	return "low"
}

// ChangeRisk scores how risky the diff itself is (0-100), independent of how
// important the touched code is. Components: size (max 30), new files (max 15),
// config/infra files (20), deletions in high/critical files (max 20) and a low
// test ratio (max 15). Factors list components worth at least 5 points.
func ChangeRisk(changes []FileChange, hm *types.Heatmap) (int, []string) {
	var total, newFiles, hotDeleted, codeAdded, testAdded int
	var infra []string
	for _, c := range changes {
		if isDocFile(c.Path) {
			continue
		}
		total += c.LinesAdded + c.LinesDeleted
		switch {
		case isTestFile(c.Path):
			testAdded += c.LinesAdded
		case isInfraFile(c.Path):
			infra = append(infra, c.Path)
		default:
			codeAdded += c.LinesAdded
			if c.IsNew {
				newFiles++
			}
		}
		if f, ok := hm.Files[c.Path]; ok && (f.Tier == types.TierHigh || f.Tier == types.TierCritical) {
			hotDeleted += c.LinesDeleted
		}
	}

	score := 0
	var factors []string
	add := func(points int, factor string) {
		score += points
		if points >= 5 {
			factors = append(factors, factor)
		}
	}
	add(min(30, total*30/500), fmt.Sprintf("%d lines changed", total))
	add(min(15, newFiles*5), fmt.Sprintf("%d new file(s)", newFiles))
	if len(infra) > 0 {
		add(20, "config/infra file(s): "+strings.Join(infra, ", "))
	}
	add(min(20, hotDeleted*20/100), fmt.Sprintf("%d lines deleted from high-stakes files", hotDeleted))
	if codeAdded > 0 {
		if ratio := float64(testAdded) / float64(codeAdded); ratio < 0.3 {
			add(int(15*(0.3-ratio)/0.3), fmt.Sprintf("low test ratio (%.2f)", ratio))
		}
	}
	return min(100, score), factors
}

// dependencyManifests are files that change what a build pulls in.
var dependencyManifests = map[string]bool{
	"go.mod": true, "package.json": true, "requirements.txt": true, "pyproject.toml": true,
	"Pipfile": true, "Cargo.toml": true, "Gemfile": true, "pom.xml": true,
	"build.gradle": true, "build.gradle.kts": true, "settings.gradle": true, "settings.gradle.kts": true,
	"Cargo.lock": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"poetry.lock": true, "Pipfile.lock": true, "uv.lock": true, "Gemfile.lock": true,
}

// isDependencyManifest reports dependency manifests and lock files, including
// requirements variants such as requirements-dev.txt.
func isDependencyManifest(base string) bool {
	return dependencyManifests[base] || (strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt"))
}

// isInfraFile reports CI, container, deployment, IaC and dependency manifests.
// go.sum (a checksum list that moves with go.mod) and test fixtures are excluded as noise.
func isInfraFile(p string) bool {
	p = filepath.ToSlash(p)
	if strings.Contains(p, "testdata/") || strings.Contains(p, "fixtures/") {
		return false
	}
	if strings.Contains(p, ".github/workflows/") {
		return true
	}
	base := filepath.Base(p)
	if isDependencyManifest(base) {
		return true
	}
	switch base {
	case "Dockerfile", "Containerfile", "Makefile", "Chart.yaml":
		return true
	}
	if strings.HasPrefix(base, "Dockerfile.") || strings.HasPrefix(base, "Containerfile.") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".yaml", ".yml", ".toml", ".tf", ".tfvars", ".hcl":
		return true
	}
	return false
}

// SuggestReviewers ranks owners of the hottest changed files (top 5, docs
// skipped) by sum of ownership share times file heat, excluding the author
// (matched by email or handle, case-insensitive). Returns at most n reviewers.
func SuggestReviewers(risk types.PRRisk, owners map[string][]ownership.Owner, author string, n int) []types.Reviewer {
	if n <= 0 {
		return nil
	}
	files := make([]types.FileChange, 0, len(risk.FilesChanged))
	for _, fc := range risk.FilesChanged {
		if !isDocFile(fc.Path) {
			files = append(files, fc)
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].HeatScore > files[j].HeatScore })
	if len(files) > 5 {
		files = files[:5]
	}

	author = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(author)), "@")
	isAuthor := func(o ownership.Owner) bool {
		return author != "" && (strings.ToLower(o.Email) == author || strings.TrimPrefix(strings.ToLower(o.Handle), "@") == author)
	}

	byHandle := map[string]*types.Reviewer{}
	for _, fc := range files {
		weight := float64(max(fc.HeatScore, 1))
		for _, o := range owners[fc.Path] {
			if o.Handle == "" || isAuthor(o) {
				continue
			}
			r := byHandle[o.Handle]
			if r == nil {
				r = &types.Reviewer{Handle: o.Handle, Email: o.Email}
				byHandle[o.Handle] = r
			}
			r.Score += o.Share * weight
			r.Files = append(r.Files, fc.Path)
		}
	}

	reviewers := make([]types.Reviewer, 0, len(byHandle))
	for _, r := range byHandle {
		r.Score = math.Round(r.Score*10) / 10
		reviewers = append(reviewers, *r)
	}
	sort.Slice(reviewers, func(i, j int) bool {
		if reviewers[i].Score != reviewers[j].Score {
			return reviewers[i].Score > reviewers[j].Score
		}
		return reviewers[i].Handle < reviewers[j].Handle
	})
	if len(reviewers) > n {
		reviewers = reviewers[:n]
	}
	return reviewers
}

// ApplyAutonomy sets each changed file's agent autonomy and the PR's:
// the lowest level over its files, auto-merge only at L3, and why. Files
// in the heatmap (or unscored, with an inherited tier) follow
// scorer.Autonomy; inert files (docs, images, testdata) are L3 and config
// files (manifests, lock files, tool dotfiles) L1. maxLevel caps all.
func ApplyAutonomy(risk *types.PRRisk, hm *types.Heatmap, maxLevel int) {
	risk.AutonomyLevel, risk.AutonomyReason = maxLevel, ""
	for i := range risk.FilesChanged {
		fc := &risk.FilesChanged[i]
		_, inHeatmap := hm.Files[fc.Path]
		a, cause := fileAutonomy(*fc, inHeatmap || fc.Unscored)
		if a.Level > maxLevel {
			a.Level, a.Reason, cause = maxLevel, a.Reason+fmt.Sprintf("; capped at L%d by autonomy.max_level", maxLevel),
				fmt.Sprintf("autonomy.max_level is %d", maxLevel)
		}
		a.Label = types.AutonomyLabel(a.Level)
		fc.Autonomy = &a
		if a.Level < risk.AutonomyLevel || (a.Level == risk.AutonomyLevel && causeRank(cause) > causeRank(risk.AutonomyReason)) {
			risk.AutonomyLevel, risk.AutonomyReason = a.Level, cause
		}
	}
	risk.AutoMergeAllowed = risk.AutonomyLevel == types.MaxAutonomy
	if risk.AutoMergeAllowed {
		risk.AutonomyReason = "every touched file is L3"
	} else if risk.AutonomyReason == "" {
		risk.AutonomyReason = fmt.Sprintf("autonomy.max_level is %d", maxLevel)
	}
}

const (
	causeUnassessed = "touches files whose impact is not AI-assessed"
	causeNotScored  = "touches files HighStakes has not assessed"
	causeConfig     = "touches dependency or tool config files"
)

// causeRank orders the causes that can set the PR's autonomy level, so a tie
// quotes the most important: Harden now or Protect files, then impact not
// AI-assessed, then new, unscored or config files, then the max_level cap.
// "" (no cause yet) ranks lowest.
func causeRank(cause string) int {
	switch cause {
	case "touches " + types.QuadrantFragileCritical.Label() + " files", "touches " + types.QuadrantProtect.Label() + " files":
		return 5
	case causeUnassessed:
		return 4
	case causeNotScored, causeConfig:
		return 3
	case "":
		return 0
	}
	if strings.HasPrefix(cause, "autonomy.max_level") {
		return 1
	}
	return 2 // other postures (Stabilize)
}

// fileAutonomy is one changed file's autonomy before the config cap, and
// the cause to quote when it sets the PR's level ("" for L3 files). scored
// is false for the non-code files CalculatePRRisk leaves unscored.
func fileAutonomy(fc types.FileChange, scored bool) (types.Autonomy, string) {
	switch {
	case !scored && isConfigFile(fc.Path):
		return types.Autonomy{Level: 1, Reason: "dependency or tool config: propose only"}, causeConfig
	case !scored && isPlainTextDoc(fc.Path):
		return types.Autonomy{Level: types.MaxAutonomy, Reason: "plain-text docs: cannot execute"}, ""
	case !scored:
		// Never assessed: an SVG can carry scripts and test fixtures can be
		// executed, so unknown content gets no autonomy beyond proposing.
		return types.Autonomy{Level: 1, Reason: "not assessed: propose only"}, causeNotScored
	}
	q := fc.Quadrant
	if q == "" {
		q = scorer.TierQuadrant(fc.Tier)
	}
	assessed := fc.ImpactAssessed && !fc.Unscored
	a := scorer.Autonomy(q, assessed, types.MaxAutonomy)
	switch {
	case a.Level == types.MaxAutonomy:
		return a, ""
	case !assessed && q.Autonomy().Level > 1: // the L1 cap set the level
		return a, causeUnassessed
	}
	return a, "touches " + q.Label() + " files"
}

// isPlainTextDoc reports docs that cannot execute: Markdown and
// reStructuredText. MDX can embed code and .txt can be a build input
// (CMakeLists.txt, requirements.txt), so neither qualifies.
func isPlainTextDoc(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".rst":
		return true
	}
	return false
}
