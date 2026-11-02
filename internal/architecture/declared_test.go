package architecture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content *string // nil: no file
		want    types.DeclaredArchitecture
		found   bool
		wantErr string
	}{
		{name: "missing file is empty, not an error", content: nil, found: false},
		{
			name: "links and placement",
			content: ptr(`links:
  - from: sup
    to: gw
    label: gRPC control session
    both: true
    evidence: docs/a.mdx:59
placement:
  - unit: sbx
    runs_in: workload pod
`),
			found: true,
			want: types.DeclaredArchitecture{
				Links:     []types.DeclaredLink{{From: "sup", To: "gw", Label: "gRPC control session", Both: true, Evidence: "docs/a.mdx:59"}},
				Placement: []types.DeclaredPlacement{{Unit: "sbx", RunsIn: "workload pod"}},
			},
		},
		{name: "empty file", content: ptr(""), found: true},
		{name: "unknown field is rejected", content: ptr("links:\n  - from: a\n    too: b\n"), found: true, wantErr: "field too not found"},
		{name: "malformed yaml", content: ptr("links: [\n"), found: true, wantErr: "architecture.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "architecture.yaml")
			if tt.content != nil {
				if err := os.WriteFile(p, []byte(*tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, found, err := Load(p)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if len(got.Links) != len(tt.want.Links) || len(got.Placement) != len(tt.want.Placement) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got.Links {
				if got.Links[i] != tt.want.Links[i] {
					t.Errorf("link %d = %+v, want %+v", i, got.Links[i], tt.want.Links[i])
				}
			}
			for i := range got.Placement {
				if got.Placement[i] != tt.want.Placement[i] {
					t.Errorf("placement %d = %+v, want %+v", i, got.Placement[i], tt.want.Placement[i])
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	names := []string{"gw", "sbx", "sup"}
	tests := []struct {
		name string
		d    types.DeclaredArchitecture
		want []string // substrings of the error; nil = valid
	}{
		{name: "empty is valid", d: types.DeclaredArchitecture{}},
		{
			name: "known units",
			d: types.DeclaredArchitecture{
				Links:     []types.DeclaredLink{{From: "sup", To: "gw"}},
				Placement: []types.DeclaredPlacement{{Unit: "sbx", RunsIn: "workload pod"}},
			},
		},
		{
			name: "unknown unit lists valid names",
			d:    types.DeclaredArchitecture{Links: []types.DeclaredLink{{From: "supervisor", To: "gw"}}},
			want: []string{`links[0].from: unknown unit "supervisor"`, "valid units: gw, sbx, sup"},
		},
		{
			name: "missing fields and self link",
			d: types.DeclaredArchitecture{
				Links:     []types.DeclaredLink{{From: "gw"}, {From: "sup", To: "sup"}},
				Placement: []types.DeclaredPlacement{{Unit: "sbx"}},
			},
			want: []string{"links[0].to: required", "links[1]: from and to are the same unit", "placement[0].runs_in: required"},
		},
		{
			name: "duplicate placement",
			d: types.DeclaredArchitecture{Placement: []types.DeclaredPlacement{
				{Unit: "sbx", RunsIn: "a"}, {Unit: "sbx", RunsIn: "b"},
			}},
			want: []string{`placement[1]: unit "sbx" already placed`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.d, names)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %q, missing %q", err, w)
				}
			}
		})
	}
}

func ptr(s string) *string { return &s }
