package units

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

var (
	dockerRun = regexp.MustCompile(`(?i)^\s*(ENTRYPOINT|CMD)\s+(.+)$`)
	yamlImage = regexp.MustCompile(`^\s*-?\s*(image|repository):\s*["']?([^"'\s#]+)`)
	yamlTop   = regexp.MustCompile(`^([A-Za-z0-9_.-]+):`)
	shells    = map[string]bool{"sh": true, "bash": true, "ash": true, "dash": true, "zsh": true, "env": true, "tini": true}
)

// Placements scans fsys for deployment hints naming a unit: the program in a
// Dockerfile ENTRYPOINT or CMD, and container images in Helm values or
// Kubernetes manifests whose name equals the unit name or ends with
// "-<image name>". Files under tests/ are skipped. It returns hints per unit name, sorted by evidence. These
// are inferences: a hint says where a binary is packaged, not that it runs.
func Placements(fsys fs.FS, skipDirs []string, names []string) (map[string][]types.Placement, error) {
	out := map[string][]types.Placement{}
	if len(names) == 0 {
		return out, nil
	}
	files, err := walk(fsys, skipDirs)
	if err != nil {
		return nil, err
	}
	charts := map[string]bool{}
	for _, f := range files {
		if path.Base(f) == "Chart.yaml" {
			charts[path.Dir(f)] = true
		}
	}
	add := func(name string, p types.Placement) { out[name] = append(out[name], p) }

	for _, f := range files {
		if tour.IsTestFile(f) {
			continue // test fixtures are not deployments
		}
		base := path.Base(f)
		switch {
		case isDockerfile(base):
			data, _ := fs.ReadFile(fsys, f) // unreadable: no hints
			for i, line := range strings.Split(string(data), "\n") {
				m := dockerRun.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				prog := path.Base(dockerProgram(m[2]))
				for _, n := range names {
					if prog == n {
						add(n, types.Placement{Kind: "dockerfile", Detail: strings.ToUpper(m[1]) + " of " + f, Evidence: fmt.Sprintf("%s:%d", f, i+1)})
					}
				}
			}
		case path.Ext(base) == ".yaml" || path.Ext(base) == ".yml":
			chart := chartOf(f, charts)
			data, _ := fs.ReadFile(fsys, f) // unreadable: no hints
			top := ""
			for i, line := range strings.Split(string(data), "\n") {
				if m := yamlTop.FindStringSubmatch(line); m != nil {
					top = m[1]
				}
				m := yamlImage.FindStringSubmatch(line)
				if m == nil || strings.Contains(line, "{{") {
					continue
				}
				img := imageName(m[2])
				if len(img) < 3 {
					continue
				}
				for _, n := range names {
					if n != img && !strings.HasSuffix(n, "-"+img) {
						continue
					}
					p := types.Placement{Kind: "k8s", Detail: "image " + m[2] + " in " + f, Evidence: fmt.Sprintf("%s:%d", f, i+1)}
					if chart != "" {
						p.Kind, p.Detail = "helm", fmt.Sprintf("helm chart %s: %s image %s", chart, top, m[2])
					}
					add(n, p)
				}
			}
		}
	}
	for n := range out {
		sort.Slice(out[n], func(i, j int) bool { return out[n][i].Evidence < out[n][j].Evidence })
	}
	return out, nil
}

func isDockerfile(base string) bool {
	l := strings.ToLower(base)
	if strings.HasSuffix(l, ".dockerignore") {
		return false
	}
	return strings.HasPrefix(l, "dockerfile") || strings.HasPrefix(l, "containerfile") || strings.HasSuffix(l, ".dockerfile")
}

// dockerProgram returns the program an ENTRYPOINT or CMD runs, or "" when it
// is a shell wrapper (the real program is then inside a string).
func dockerProgram(arg string) string {
	var argv []string
	if json.Unmarshal([]byte(strings.TrimSpace(arg)), &argv) != nil {
		argv = strings.Fields(arg)
	}
	if len(argv) == 0 || shells[path.Base(argv[0])] {
		return ""
	}
	return argv[0]
}

// chartOf returns the Helm chart directory holding f, or "".
func chartOf(f string, charts map[string]bool) string {
	for d := path.Dir(f); d != "." && d != "/"; d = path.Dir(d) {
		if charts[d] {
			return d
		}
	}
	if charts["."] {
		return "."
	}
	return ""
}

// imageName returns an image reference's last path segment without tag or
// digest: "ghcr.io/acme/app:1.2@sha256:x" -> "app".
func imageName(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	ref = ref[strings.LastIndex(ref, "/")+1:]
	if i := strings.Index(ref, ":"); i >= 0 {
		ref = ref[:i]
	}
	return ref
}
