package units

import (
	"io/fs"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Apply detects the entry points in fsys (rooted at the repo), links them
// through hm's import graph, attaches deployment hints and stores the units
// in hm.Metadata.Units. It also sets every file's Component and Units.
func Apply(hm *types.Heatmap, fsys fs.FS, skipDirs []string) error {
	entries, err := Detect(fsys, skipDirs)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(hm.Files))
	for p := range hm.Files {
		paths = append(paths, p)
	}
	markers := tour.Markers(paths, func(p string) bool {
		_, err := fs.Stat(fsys, p)
		return err == nil
	})
	us := Link(hm, entries, markers)

	names := make([]string, len(us))
	for i, u := range us {
		names[i] = u.Name
	}
	hints, err := Placements(fsys, skipDirs, names)
	if err != nil {
		return err
	}
	for i := range us {
		us[i].InferredPlacement = hints[us[i].Name]
	}
	hm.Metadata.Units = us
	return nil
}
