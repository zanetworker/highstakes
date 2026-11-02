package policy

import (
	"sort"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

func file(path string, heat int, tier types.Tier) *types.FileHeat {
	fh := &types.FileHeat{Path: path, HeatScore: heat, Tier: tier}
	fh.Factors.BlastRadius.CriticalReason = "reason for " + path
	return fh
}

var th = scorer.DefaultTierThresholds()

func testHeatmap() *types.Heatmap {
	return &types.Heatmap{Files: map[string]*types.FileHeat{
		"auth/jwt.go":       file("auth/jwt.go", 95, types.TierCritical),
		"auth/session.go":   file("auth/session.go", 70, types.TierHigh),
		"auth/oidc/oidc.go": file("auth/oidc/oidc.go", 65, types.TierHigh),
		"api/handler.go":    file("api/handler.go", 80, types.TierHigh),
		"api/format.go":     file("api/format.go", 10, types.TierLow),
		"billing/charge.go": file("billing/charge.go", 40, types.TierMedium),
		"main.go":           file("main.go", 90, types.TierCritical),
		"single/only.go":    file("single/only.go", 75, types.TierHigh),
	}}
}

func TestParseTier(t *testing.T) {
	tests := []struct {
		in      string
		want    types.Tier
		wantErr bool
	}{
		{"high", types.TierHigh, false},
		{"CRITICAL", types.TierCritical, false},
		{"low", types.TierLow, false},
		{"", "", true},
		{"severe", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTier(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "critical, high, medium, low") {
				t.Errorf("error should list valid values: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHotPaths(t *testing.T) {
	tests := []struct {
		name    string
		minTier types.Tier
		want    map[string]bool // path -> isDir
	}{
		{
			name:    "high collapses fully hot dir, keeps files in mixed dir",
			minTier: types.TierHigh,
			want:    map[string]bool{"auth/": true, "api/handler.go": false, "main.go": false, "single/only.go": false},
		},
		{
			name:    "critical only lists critical files",
			minTier: types.TierCritical,
			want:    map[string]bool{"auth/jwt.go": false, "main.go": false},
		},
		{
			name:    "low covers everything but never collapses the repo root",
			minTier: types.TierLow,
			want:    map[string]bool{"auth/": true, "api/": true, "billing/charge.go": false, "main.go": false, "single/only.go": false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HotPaths(testHeatmap(), tt.minTier, th)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries %+v, want %v", len(got), got, tt.want)
			}
			for _, e := range got {
				isDir, ok := tt.want[e.Path]
				if !ok || isDir != e.IsDir {
					t.Errorf("unexpected entry %+v", e)
				}
			}
			for i := 1; i < len(got); i++ {
				if got[i-1].HeatScore < got[i].HeatScore {
					t.Errorf("entries not sorted by heat desc: %v", got)
				}
			}
		})
	}

	t.Run("dir entry carries max heat, tier and files", func(t *testing.T) {
		for _, e := range HotPaths(testHeatmap(), types.TierHigh, th) {
			if e.Path == "auth/" {
				if e.HeatScore != 95 || e.Tier != types.TierCritical || len(e.Files) != 3 {
					t.Errorf("auth/ entry = %+v", e)
				}
			}
		}
	})

	t.Run("empty heatmap", func(t *testing.T) {
		if got := HotPaths(&types.Heatmap{Files: map[string]*types.FileHeat{}}, types.TierHigh, th); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})
}

func TestCodeowners(t *testing.T) {
	entries := []Entry{
		{Path: "auth/", IsDir: true, Tier: types.TierCritical, HeatScore: 95, Files: []string{"auth/jwt.go", "auth/session.go"}},
		{Path: "main.go", Tier: types.TierCritical, HeatScore: 90, Files: []string{"main.go"}},
		{Path: "orphan.go", Tier: types.TierHigh, HeatScore: 70, Files: []string{"orphan.go"}},
	}
	owners := map[string][]ownership.Owner{
		"auth/jwt.go":     {{Handle: "@alice", Share: 0.9}, {Handle: "@bob", Share: 0.1}},
		"auth/session.go": {{Handle: "@bob", Share: 0.6}, {Handle: "@carol", Share: 0.4}},
		"main.go":         {{Handle: "dave@x.com", Share: 1}},
	}

	block, rows, _ := Codeowners(entries, owners, 2)

	t.Run("dir owners aggregate across files", func(t *testing.T) {
		if got := rows[0].Owners; len(got) != 2 || got[0] != "@alice" || got[1] != "@bob" {
			t.Errorf("auth/ owners = %v, want [@alice @bob]", got)
		}
	})
	t.Run("block has markers and anchored paths", func(t *testing.T) {
		for _, want := range []string{BeginMarker, EndMarker, "/auth/ @alice @bob", "/main.go dave@x.com"} {
			if !strings.Contains(block, want) {
				t.Errorf("block missing %q:\n%s", want, block)
			}
		}
	})
	t.Run("ownerless path is commented, not an empty rule", func(t *testing.T) {
		if !strings.Contains(block, "# /orphan.go") || strings.Contains(block, "\n/orphan.go") {
			t.Errorf("orphan should be a comment:\n%s", block)
		}
		if len(rows[2].Owners) != 0 {
			t.Errorf("orphan owners = %v", rows[2].Owners)
		}
	})
	t.Run("maxOwners one limits list", func(t *testing.T) {
		_, rows, _ := Codeowners(entries, owners, 1)
		if len(rows[0].Owners) != 1 {
			t.Errorf("owners = %v, want 1", rows[0].Owners)
		}
	})
}

func TestReplaceBlock(t *testing.T) {
	block := BeginMarker + "\n/a.go @x\n" + EndMarker + "\n"
	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{"empty file gets block", "", block},
		{"appends after existing rules", "* @team\n", "* @team\n\n" + block},
		{"appends newline when missing", "* @team", "* @team\n\n" + block},
		{
			"replaces existing block, keeps surroundings",
			"* @team\n" + BeginMarker + "\n/old.go @y\n" + EndMarker + "\n/docs/ @docs\n",
			"* @team\n" + block + "/docs/ @docs\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReplaceBlock(tt.existing, block); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	entries := HotPaths(testHeatmap(), types.TierHigh, th)
	p := Build(testHeatmap(), entries, types.TierHigh)

	if p.MinTier != types.TierHigh || p.GeneratedBy != "highstakes" {
		t.Errorf("header = %+v", p)
	}
	globs := strings.Join(p.Paths, ",")
	for _, want := range []string{"auth/**", "api/handler.go", "main.go"} {
		if !strings.Contains(globs, want) {
			t.Errorf("paths %v missing %s", p.Paths, want)
		}
	}
	if len(p.Files) != 6 {
		t.Errorf("files = %d, want 6 (every hot file listed)", len(p.Files))
	}
	if p.Files[0].Path != "auth/jwt.go" || p.Files[0].Reason != "reason for auth/jwt.go" {
		t.Errorf("first file = %+v, want hottest with reason", p.Files[0])
	}

	t.Run("no hot files gives empty lists not nil", func(t *testing.T) {
		p := Build(testHeatmap(), nil, types.TierCritical)
		if p.Paths == nil || p.Files == nil {
			t.Errorf("want empty slices for stable JSON, got %+v", p)
		}
	})
}

func TestHotPaths_ByImpact(t *testing.T) {
	assessed := func(path string, impact int, tier types.Tier) *types.FileHeat {
		fh := file(path, impact, tier)
		fh.Impact, fh.ImpactAssessed = impact, true
		return fh
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"core/stable.go": assessed("core/stable.go", 90, types.TierHigh),        // protect: impact 90, stable
		"core/edge.go":   assessed("core/edge.go", 61, types.TierHigh),          // impact at the high threshold
		"ui/churn.go":    assessed("ui/churn.go", 40, types.TierMedium),         // watch: likely, low impact
		"pay/hot.go":     assessed("pay/hot.go", 70, types.TierCritical),        // fragile-critical, impact below 86
		"lib/static.go":  file("lib/static.go", 70, types.TierHigh),             // no LLM: tier as today
		"lib/est.go":     {Path: "lib/est.go", Impact: 95, Tier: types.TierLow}, // estimate is ignored
	}}
	tests := []struct {
		name    string
		minTier types.Tier
		want    []string
	}{
		{"high: impact >= 61 or tier high", types.TierHigh, []string{"core/", "lib/static.go", "pay/hot.go"}},
		{"critical: impact >= 86, plus critical tier", types.TierCritical, []string{"core/stable.go", "pay/hot.go"}},
		{"medium adds likely files by tier and impact >= 31", types.TierMedium, []string{"core/", "lib/static.go", "pay/hot.go", "ui/churn.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, e := range HotPaths(hm, tt.minTier, th) {
				got = append(got, e.Path)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

// GitHub documents no backslash escaping in CODEOWNERS (escaping a leading
// # and [ ] ranges are listed as unsupported), so a path with whitespace or a
// glob character cannot be written as a rule matching only that path.
func TestCodeowners_UnrepresentablePaths(t *testing.T) {
	tests := []struct {
		name, path, wantLine string
		wantUnrepresentable  bool
	}{
		{"plain path is a rule", "auth/jwt.go", "/auth/jwt.go @alice", false},
		{"hash and bang after the leading slash are literal", "a/#notes!.md", "/a/#notes!.md @alice", false},
		{"space cannot be expressed", "src/payment handlers.go", "# cannot express in CODEOWNERS (space in path): src/payment handlers.go", true},
		{"tab counts as space", "a\tb.go", "# cannot express in CODEOWNERS (space in path): a\tb.go", true},
		{"glob star cannot be expressed", "a/*.go", "# cannot express in CODEOWNERS (glob character in path): a/*.go", true},
		{"bracket cannot be expressed", "a/[x].go", "# cannot express in CODEOWNERS (glob character in path): a/[x].go", true},
		{"backslash cannot be expressed", `a\b.go`, `# cannot express in CODEOWNERS (glob character in path): a\b.go`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := []Entry{{Path: tt.path, Tier: types.TierCritical, HeatScore: 90, Files: []string{tt.path}}}
			owners := map[string][]ownership.Owner{tt.path: {{Handle: "@alice", Share: 1}}}
			block, rows, unrep := Codeowners(entries, owners, 2)
			if !strings.Contains(block, "\n"+tt.wantLine+"\n") {
				t.Errorf("block missing %q:\n%s", tt.wantLine, block)
			}
			if tt.wantUnrepresentable {
				if len(unrep) != 1 || unrep[0] != tt.path || len(rows) != 0 {
					t.Errorf("unrepresentable = %v rows = %v, want [%q] and no rows", unrep, rows, tt.path)
				}
			} else if len(unrep) != 0 || len(rows) != 1 {
				t.Errorf("unrepresentable = %v rows = %d, want none and one row", unrep, len(rows))
			}
		})
	}
	t.Run("no entries gives empty list not nil", func(t *testing.T) {
		if _, _, unrep := Codeowners(nil, nil, 2); unrep == nil {
			t.Error("want empty slice for stable JSON")
		}
	})
}

func TestBuild_PathsStayUnescaped(t *testing.T) {
	path := "src/payment handlers.go"
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{path: file(path, 90, types.TierCritical)}}
	p := Build(hm, HotPaths(hm, types.TierHigh, th), types.TierHigh)
	if len(p.Paths) != 1 || p.Paths[0] != path || p.Files[0].Path != path {
		t.Errorf("policy paths = %v files = %+v, want raw %q", p.Paths, p.Files, path)
	}
}

func TestAgents(t *testing.T) {
	assessed := func(q types.Quadrant) *types.FileHeat {
		return &types.FileHeat{ImpactAssessed: true, Quadrant: q}
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"ui/a.go":     assessed(types.QuadrantLow),
		"ui/b.go":     assessed(types.QuadrantLow),
		"ui/w/c.go":   assessed(types.QuadrantLow),
		"core/x.go":   assessed(types.QuadrantProtect),
		"core/y.go":   assessed(types.QuadrantLow),
		"core/s.go":   assessed(types.QuadrantWatch),
		"main.go":     assessed(types.QuadrantFragileCritical),
		"est/u.go":    {Tier: types.TierLow}, // not assessed: capped at L1
		"est/v.go":    {Tier: types.TierMedium},
		"solo/one.go": assessed(types.QuadrantLow), // one file never collapses
	}}
	paths := func(p AgentPolicy) map[int]string {
		out := map[int]string{}
		for _, l := range p.Levels {
			out[l.Level] = strings.Join(l.Paths, ",")
		}
		return out
	}
	t.Run("levels with collapsed directories", func(t *testing.T) {
		p := Agents(hm, 3)
		want := map[int]string{
			3: "core/y.go,solo/one.go,ui/a.go,ui/b.go,ui/w/c.go",
			2: "core/s.go",
			1: "core/x.go,est/**",
			0: "main.go",
		}
		got := paths(p)
		for lvl, w := range want {
			if got[lvl] != w {
				t.Errorf("L%d paths = %q, want %q", lvl, got[lvl], w)
			}
		}
		if len(p.Levels) != 4 || p.Levels[0].Level != 3 || p.Levels[0].Label != "auto-merge" || p.Levels[0].Meaning == "" {
			t.Errorf("levels = %+v, want 4 described levels, highest first", p.Levels)
		}
		if p.MaxLevel != 3 || p.DefaultLevel != 1 || p.HowToRead == "" || p.CapRule == "" {
			t.Errorf("header = %+v", p)
		}
	})
	t.Run("elevated levels list exact files, never directory globs", func(t *testing.T) {
		two := &types.Heatmap{Files: map[string]*types.FileHeat{
			"app/a.go": assessed(types.QuadrantLow),
			"app/b.go": assessed(types.QuadrantLow),
			"svc/a.go": assessed(types.QuadrantWatch),
			"svc/b.go": assessed(types.QuadrantWatch),
		}}
		got := paths(Agents(two, 3))
		if got[3] != "app/a.go,app/b.go" {
			t.Errorf("L3 paths = %q, want exact files", got[3])
		}
		if got[2] != "svc/a.go,svc/b.go" {
			t.Errorf("L2 paths = %q, want exact files", got[2])
		}
	})
	t.Run("header says unlisted paths are propose only", func(t *testing.T) {
		p := Agents(hm, 3)
		if !strings.Contains(p.HowToRead, "not listed") || !strings.Contains(p.HowToRead, "propose only") {
			t.Errorf("how_to_read = %q, want the unlisted default spelled out", p.HowToRead)
		}
	})
	t.Run("max level caps everything", func(t *testing.T) {
		got := paths(Agents(hm, 1))
		if got[3] != "" || got[2] != "" {
			t.Errorf("nothing may sit above the cap: %v", got)
		}
		if got[1] != "core/**,est/**,solo/one.go,ui/**" {
			t.Errorf("capped files should land at L1: %q", got[1])
		}
	})
	t.Run("empty heatmap has empty path lists", func(t *testing.T) {
		p := Agents(&types.Heatmap{Files: map[string]*types.FileHeat{}}, 3)
		for _, l := range p.Levels {
			if l.Paths == nil || len(l.Paths) != 0 {
				t.Errorf("L%d paths = %#v, want empty list", l.Level, l.Paths)
			}
		}
	})
}
