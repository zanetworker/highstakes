package units

import (
	"io/fs"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Apply detects the entry points in fsys (rooted at the repo), links them
// through hm's import graph, attaches deployment hints and stores the units
// in hm.Metadata.Units. It also sets every file's Component and Units, and
// marks supporting units (see MarkSupporting; supporting holds the
// tour.supporting globs from config). Entries whose file is not in hm are
// dropped: the analyzer left them out (excluded or gitignored), so they are
// not part of the analyzed repo.
func Apply(hm *types.Heatmap, fsys fs.FS, skipDirs, supporting []string) error {
	found, err := detect(fsys, skipDirs)
	if err != nil {
		return err
	}
	files := slashFiles(hm)
	var entries []Entry
	for _, e := range found {
		if _, ok := files[e.Entry]; ok {
			entries = append(entries, e)
		}
	}
	entries = uniqueNames(entries)
	paths := make([]string, 0, len(files))
	for p := range files {
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
	MarkSupporting(us, supporting)
	hm.Metadata.Units = us
	return nil
}
