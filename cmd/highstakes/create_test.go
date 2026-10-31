package main

import "testing"

func TestNoFilesHint(t *testing.T) {
	tests := []struct {
		minTier string
		want    string
	}{
		{"critical", "No files at or above tier critical. Try --min-tier high, or run analyze with an LLM."},
		{"high", "No files at or above tier high. Try --min-tier medium, or run analyze with an LLM."},
		{"medium", "No files at or above tier medium. Try --min-tier low, or run analyze with an LLM."},
		{"low", "No files at or above tier low. Run analyze first, with an LLM for meaningful tiers."},
	}
	for _, tt := range tests {
		t.Run(tt.minTier, func(t *testing.T) {
			if got := noFilesHint(tt.minTier); got != tt.want {
				t.Errorf("noFilesHint(%q) =\n %q\nwant\n %q", tt.minTier, got, tt.want)
			}
		})
	}
}
