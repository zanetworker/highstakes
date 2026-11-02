package calibration

import (
	"fmt"
	"regexp"
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
	securityIDs = []string{"cve-", "ghsa-"} // matched anywhere in the message

	// Subject-only keywords. "security" must be a standalone word, so header
	// names and identifiers (Content-Security-Policy, podSecurityContext,
	// SecurityException, JAVA_SECURITY_DEBUG) and SECURITY.md do not count.
	securityWord = regexp.MustCompile(`(^|[^a-z0-9_.-])security([^a-z0-9_.-]|\.?$)`)
	vulnWord     = regexp.MustCompile(`(^|[^a-z0-9_])vuln`) // vuln, vulnerability, ReDoS-vulnerable

	// Conventional-commit types that do not change shipped code. Their subjects
	// mention security for docs, CI scans and tests, not for fixes.
	nonCodeType = regexp.MustCompile(`^(docs|ci|test|tests|chore|build|style)(\([^)]*\))?!?:`)
)

// ClassifyMessage labels a commit message. Bug-fix and security keywords are
// matched in the subject line only, because bodies mention "fix" or "security"
// in passing far too often; advisory IDs and the "This reverts commit" trailer
// count anywhere. Security keywords are ignored in docs, ci, test, chore,
// build and style commits. A revert is not also counted as a bug fix, because its title
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
	if !nonCodeType.MatchString(lowerSubject) && (securityWord.MatchString(lowerSubject) || vulnWord.MatchString(lowerSubject)) {
		l.Security = true
	}
	return l
}

// EventFilter selects which event kinds a calibration run counts.
type EventFilter string

const (
	EventsAll      EventFilter = "all"      // reverts, bug fixes, security fixes and incidents
	EventsFixes    EventFilter = "fixes"    // bug-fix and security commits
	EventsReverts  EventFilter = "reverts"  // reverts only
	EventsSecurity EventFilter = "security" // security fixes only, a proxy for impact
)

// ParseEventFilter parses an --events value (case-insensitive).
func ParseEventFilter(s string) (EventFilter, error) {
	switch f := EventFilter(strings.ToLower(s)); f {
	case EventsAll, EventsFixes, EventsReverts, EventsSecurity:
		return f, nil
	}
	return "", fmt.Errorf("unknown event kind %q (valid: all, fixes, reverts, security)", s)
}

// apply keeps only the labels the filter counts.
func (f EventFilter) apply(l Labels) Labels {
	switch f {
	case EventsFixes:
		l.Revert = false
	case EventsReverts:
		l = Labels{Revert: l.Revert}
	case EventsSecurity:
		l = Labels{Security: l.Security}
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
// returns the counts per file, keeping only the kinds the filter selects
// (incidents count only for EventsAll). Commits touching more than
// MaxFilesPerEvent files are skipped. Files without events are absent.
func CountEvents(commits []gitanalyzer.CommitInfo, incidents []types.Incident, from, to time.Time, filter EventFilter) map[string]*FileEvents {
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
		l := filter.apply(ClassifyMessage(c.Message))
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
		if filter != EventsAll || !inWindow(inc.Date) {
			continue
		}
		e := get(inc.File)
		e.Total++
		e.Incident++
	}
	return out
}
