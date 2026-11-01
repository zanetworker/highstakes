// Package architecture builds generated architecture pictures from a
// heatmap's runtime units, and reads .heatmap/architecture.yaml, where
// humans declare runtime links and placements imports cannot see.
package architecture

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
	"gopkg.in/yaml.v3"
)

// ErrInvalid means architecture.yaml is malformed or names unknown units.
var ErrInvalid = errors.New("invalid architecture declaration")

// Load reads a declaration file. A missing file is not an error: it returns
// an empty declaration and found=false. Unknown fields are rejected so a
// typo does not silently drop a link.
func Load(path string) (d types.DeclaredArchitecture, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, false, nil
	}
	if err != nil {
		return d, false, fmt.Errorf("read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil && !errors.Is(err, io.EOF) {
		return d, true, fmt.Errorf("%w: %s: %v", ErrInvalid, path, err)
	}
	return d, true, nil
}

// Validate checks every link and placement against the unit names and
// returns one ErrInvalid error listing every problem (and the valid unit
// names when one is unknown).
func Validate(d types.DeclaredArchitecture, names []string) error {
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	var problems []string
	unknown := false
	unit := func(where, v string) {
		switch {
		case v == "":
			problems = append(problems, where+": required")
		case !known[v]:
			problems = append(problems, fmt.Sprintf("%s: unknown unit %q", where, v))
			unknown = true
		}
	}
	for i, l := range d.Links {
		unit(fmt.Sprintf("links[%d].from", i), l.From)
		unit(fmt.Sprintf("links[%d].to", i), l.To)
		if l.From != "" && l.From == l.To {
			problems = append(problems, fmt.Sprintf("links[%d]: from and to are the same unit", i))
		}
	}
	placed := map[string]bool{}
	for i, p := range d.Placement {
		unit(fmt.Sprintf("placement[%d].unit", i), p.Unit)
		if strings.TrimSpace(p.RunsIn) == "" {
			problems = append(problems, fmt.Sprintf("placement[%d].runs_in: required", i))
		}
		if placed[p.Unit] && p.Unit != "" {
			problems = append(problems, fmt.Sprintf("placement[%d]: unit %q already placed", i, p.Unit))
		}
		placed[p.Unit] = true
	}
	if len(problems) == 0 {
		return nil
	}
	msg := strings.Join(problems, "; ")
	if unknown {
		sorted := append([]string{}, names...)
		sort.Strings(sorted)
		msg += " (valid units: " + strings.Join(sorted, ", ") + ")"
	}
	return fmt.Errorf("%w: %s", ErrInvalid, msg)
}
