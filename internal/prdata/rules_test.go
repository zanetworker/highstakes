package prdata

import "testing"

func TestIsBugFix(t *testing.T) {
	tests := []struct {
		name string
		pr   PR
		want bool
	}{
		{"bug label", PR{Title: "Handle nil", Labels: []string{"kind/Bug"}}, true},
		{"regression label", PR{Title: "x", Labels: []string{"Regression"}}, true},
		{"defect label", PR{Title: "x", Labels: []string{"defect"}}, true},
		{"closing issue labelled bug", PR{Title: "x", ClosingIssueLabels: []string{"type: bug"}}, true},
		{"conventional fix", PR{Title: "fix: nil deref"}, true},
		{"conventional scoped fix", PR{Title: "fix(router)!: wrong match"}, true},
		{"uppercase Fix:", PR{Title: "Fix: crash"}, true},
		{"fix word not conventional", PR{Title: "Fixes the build docs"}, false},
		{"prefix is not fix", PR{Title: "fixture: add data"}, false},
		{"feature", PR{Title: "feat: add thing", Labels: []string{"enhancement"}}, false},
		{"empty", PR{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBugFix(tt.pr); got != tt.want {
				t.Errorf("IsBugFix(%+v) = %v, want %v", tt.pr, got, tt.want)
			}
		})
	}
}

func TestIsRevert(t *testing.T) {
	tests := []struct {
		title string
		want  bool
	}{
		{`Revert "feat: x"`, true},
		{"Revert: broken cache", true},
		{"revert lowercase", false}, // GitHub's button and git both write "Revert"
		{"Don't revert", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := IsRevert(PR{Title: tt.title}); got != tt.want {
				t.Errorf("IsRevert(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestHasSeverityLabel(t *testing.T) {
	tests := []struct {
		name string
		pr   PR
		want bool
	}{
		{"security", PR{Labels: []string{"area/security"}}, true},
		{"critical", PR{Labels: []string{"Critical"}}, true},
		{"p0", PR{Labels: []string{"P0"}}, true},
		{"priority p1", PR{Labels: []string{"priority: p1"}}, true},
		{"p10 is not p1", PR{Labels: []string{"p10"}}, false},
		{"sev1", PR{Labels: []string{"sev1"}}, true},
		{"severity high", PR{Labels: []string{"severity/high"}}, true},
		{"severity low excluded", PR{Labels: []string{"severity: low"}}, false},
		{"incident", PR{Labels: []string{"incident"}}, true},
		{"cve", PR{Labels: []string{"CVE"}}, true},
		{"vulnerability", PR{Labels: []string{"vulnerability"}}, true},
		{"on closing issue", PR{ClosingIssueLabels: []string{"security"}}, true},
		{"several words containing sev are not severity", PR{Labels: []string{"several"}}, false},
		{"plain bug", PR{Labels: []string{"bug", "p2"}}, false},
		{"none", PR{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSeverityLabel(tt.pr); got != tt.want {
				t.Errorf("HasSeverityLabel(%+v) = %v, want %v", tt.pr, got, tt.want)
			}
		})
	}
}

func TestDeepReview(t *testing.T) {
	tests := []struct {
		name               string
		reviews, approvers int
		want               bool
	}{
		{"two approvers", 2, 2, true},
		{"three reviews one approver", 3, 1, true},
		{"two reviews one approver", 2, 1, false},
		{"no reviews", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeepReview(PR{Reviews: tt.reviews, Approvers: tt.approvers}); got != tt.want {
				t.Errorf("DeepReview(%d reviews, %d approvers) = %v, want %v", tt.reviews, tt.approvers, got, tt.want)
			}
		})
	}
}
