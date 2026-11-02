package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/calibration"
)

func TestPrintCalibration_EventFilterAndWarning(t *testing.T) {
	tests := []struct {
		name     string
		report   calibration.Report
		want     []string
		wantNone []string
	}{
		{"security, too few", calibration.Report{Mode: "holdout", EventFilter: calibration.EventsSecurity, FilesWithEvents: 3, TooFewEvents: true},
			[]string{"Events counted: security", "WARNING: only 3 file(s) had security events"}, nil},
		{"all, enough", calibration.Report{Mode: "in-sample", EventFilter: calibration.EventsAll, FilesWithEvents: 40},
			[]string{"Events counted: all"}, []string{"WARNING"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printCalibration(&buf, &tt.report)
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, w := range tt.wantNone {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in:\n%s", w, out)
				}
			}
		})
	}
}

func TestPrintLabelReport(t *testing.T) {
	rho := 0.61
	r := &calibration.LabelReport{
		Rated: 12, Matched: 11, Unmatched: []string{"old.go"}, Positives: 4, BaseRate: 4.0 / 11, K: 5, TooFew: false,
		Signals: []calibration.LabelSignal{
			{Name: "blast_radius", Available: true, Files: 11, PrecisionAtK: 0.8, Lift: 2.2, Spearman: &rho},
			{Name: "heat", Available: true, Files: 11, PrecisionAtK: 0.4, Lift: 1.1},
		},
	}
	var buf bytes.Buffer
	printLabelReport(&buf, r)
	out := buf.String()
	for _, w := range []string{"Rated files: 12", "in heatmap: 11", "rated >= 4: 4", "blast_radius", "0.80", "0.61", "n/a", "not in the heatmap: old.go"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "WARNING") {
		t.Errorf("11 rated files must not warn:\n%s", out)
	}

	r.TooFew, r.Matched = true, 3
	buf.Reset()
	printLabelReport(&buf, r)
	if !strings.Contains(buf.String(), "WARNING: only 3 rated file(s)") {
		t.Errorf("too few must warn:\n%s", buf.String())
	}
}
