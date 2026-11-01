package types

// The two axes as the questions a reader asks. Every human-facing output
// uses these names; JSON keeps the likelihood and impact keys.
const (
	AxisLikelihood = "Will it break?"
	AxisImpact     = "How bad if it breaks?"
)

// AxisNote is said once wherever scores are explained.
const AxisNote = "Scores are a relative signal within this repo, not a probability."

// AxisEvidence says how far each question is backed (merged-PR calibration
// on 7 repos): likelihood predicts bug-fix PRs; impact is an LLM judgement
// with no human validation yet. Shown next to AxisNote.
const AxisEvidence = "Will it break? is validated on bug-fix history; How bad? is AI-assessed, not yet checked against maintainer ratings."

// Action scopes: what to do in this PR, and what to do about the file over time.
const (
	ScopeBeforeMerge = "before_merge"
	ScopeBacklog     = "backlog"
)

var quadrantLabels = map[Quadrant]string{
	QuadrantFragileCritical: "Harden now",
	QuadrantProtect:         "Protect",
	QuadrantWatch:           "Stabilize",
	QuadrantLow:             "Routine",
}

// Label is the posture a reader acts on ("Harden now"); "" for no quadrant.
// The quadrant value itself stays the stable machine-readable ID.
func (q Quadrant) Label() string { return quadrantLabels[q] }

// AxisLevel is HIGH when score reaches the repo cutoff, else LOW. An unknown
// (zero) cutoff never reads as HIGH.
func AxisLevel(score, cutoff int) string {
	if cutoff > 0 && score >= cutoff {
		return "HIGH"
	}
	return "LOW"
}

// TierPosture maps a tier to the posture of the quadrant that sets it, so a
// PR's verdict matches its gate (--fail-on critical blocks Harden now).
// Unknown tiers read as Routine.
func TierPosture(t Tier) string {
	switch t {
	case TierCritical:
		return QuadrantFragileCritical.Label()
	case TierHigh:
		return QuadrantProtect.Label()
	case TierMedium:
		return QuadrantWatch.Label()
	}
	return QuadrantLow.Label()
}

// Posture is a changed file's posture: its quadrant label, else the
// posture of its tier (unassessed or unscored files).
func (fc FileChange) Posture() string {
	if l := fc.Quadrant.Label(); l != "" {
		return l
	}
	return TierPosture(fc.Tier)
}

// Blocking reports a posture whose before-merge items gate the merge.
func Blocking(posture string) bool {
	return posture == QuadrantFragileCritical.Label() || posture == QuadrantProtect.Label()
}

// Top returns a copy of the first action in scope (the most important), or
// nil when there is none or e is nil.
func (e *Explanation) Top(scope string) *Action {
	if e == nil {
		return nil
	}
	for _, a := range e.Actions {
		if a.Scope == scope {
			return &a
		}
	}
	return nil
}
