package calibration

import (
	"strings"
	"time"

	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/types"
)

// Labels are the event kinds a commit message signals.
type Labels struct {
	Revert   bool
	BugFix   bool
	Security bool
}

// Any reports whether the commit counts as an event.
func (l Labels) Any() bool { return l.Revert || l.BugFix || l.Security }

// MaxFilesPerEvent skips commits that touch more files than this. Bulk
// renames, formatting and dependency vendoring would otherwise label most of
// the repository at once (SZZ studies filter large commits for the same reason).
const MaxFilesPerEvent = 50

var (
	securityIDs      = []string{"cve-", "ghsa-"}    // matched anywhere in the message
	securityKeywords = []string{"security", "vuln"} // matched in the subject only
)

// ClassifyMessage labels a commit message. Bug-fix and security keywords are
// matched in the subject line only, because bodies mention "fix" or "security"
// in passing far too often; advisory IDs and the "This reverts commit" trailer
// count anywhere. A revert is not also counted as a bug fix, because its title
// repeats the reverted commit's message.
func ClassifyMessage(msg string) Labels {
	var l Labels
	subject := strings.TrimSpace(strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0])
	l.Revert = strings.HasPrefix(subject, "Revert") || strings.Contains(msg, "This reverts commit")
	if !l.Revert {
		l.BugFix = gitanalyzer.IsBugFix(subject)
	}
	lowerMsg, lowerSubject := strings.ToLower(msg), strings.ToLower(subject)
	for _, id := range securityIDs {
		if strings.Contains(lowerMsg, id) {
			l.Security = true
		}
	}
	for _, kw := range securityKeywords {
		if strings.Contains(lowerSubject, kw) {
			l.Security = true
		}
	}
	return l
}

// FileEvents counts events for one file. Total counts each labelled commit and
// each incident once; the per-kind counts can overlap (a fix for a CVE is both
// BugFix and Security).
type FileEvents struct {
	Total    int `json:"total"`
	Revert   int `json:"revert"`
	BugFix   int `json:"bugfix"`
	Security int `json:"security"`
	Incident int `json:"incident"`
}

// CountEvents labels commits and incidents dated in the window (from, to] and
// returns the counts per file. Commits touching more than MaxFilesPerEvent
// files are skipped. Files without events are absent.
func CountEvents(commits []gitanalyzer.CommitInfo, incidents []types.Incident, from, to time.Time) map[string]*FileEvents {
	out := map[string]*FileEvents{}
	get := func(path string) *FileEvents {
		e, ok := out[path]
		if !ok {
			e = &FileEvents{}
			out[path] = e
		}
		return e
	}
	inWindow := func(t time.Time) bool { return t.After(from) && !t.After(to) }

	for _, c := range commits {
		if !inWindow(c.When) {
			continue
		}
		if len(c.Files) > MaxFilesPerEvent {
			continue
		}
		l := ClassifyMessage(c.Message)
		if !l.Any() {
			continue
		}
		for _, f := range c.Files {
			e := get(f)
			e.Total++
			if l.Revert {
				e.Revert++
			}
			if l.BugFix {
				e.BugFix++
			}
			if l.Security {
				e.Security++
			}
		}
	}
	for _, inc := range incidents {
		if !inWindow(inc.Date) {
			continue
		}
		e := get(inc.File)
		e.Total++
		e.Incident++
	}
	return out
}
