package tui

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func makeFiles() map[string]*types.FileHeat {
	return map[string]*types.FileHeat{
		"cmd/main.go":         {Path: "cmd/main.go", HeatScore: 10, Tier: types.TierLow, Size: types.FileSize{Lines: 50}},
		"internal/auth/jwt.go": {Path: "internal/auth/jwt.go", HeatScore: 95, Tier: types.TierCritical, Size: types.FileSize{Lines: 200}},
		"internal/auth/session.go": {Path: "internal/auth/session.go", HeatScore: 72, Tier: types.TierHigh, Size: types.FileSize{Lines: 150}},
		"internal/api/handler.go": {Path: "internal/api/handler.go", HeatScore: 45, Tier: types.TierMedium, Size: types.FileSize{Lines: 100}},
		"pkg/utils/format.go":  {Path: "pkg/utils/format.go", HeatScore: 5, Tier: types.TierLow, Size: types.FileSize{Lines: 30}},
	}
}

func TestBuildTree_CreatesCorrectStructure(t *testing.T) {
	root := BuildTree(makeFiles())

	if root.Path != "." {
		t.Errorf("root path should be '.', got %q", root.Path)
	}
	if !root.IsDir {
		t.Error("root should be a directory")
	}
	if len(root.Children) != 3 {
		t.Errorf("root should have 3 children (cmd, internal, pkg), got %d", len(root.Children))
	}
}

func TestBuildTree_MaxHeatPropagates(t *testing.T) {
	root := BuildTree(makeFiles())

	// Find internal/ dir
	var internal *TreeNode
	for _, c := range root.Children {
		if c.Name == "internal" {
			internal = c
			break
		}
	}

	if internal == nil {
		t.Fatal("internal/ directory not found")
	}

	// internal/ should have max heat of 95 (from auth/jwt.go)
	if internal.MaxHeat != 95 {
		t.Errorf("internal/ max heat should be 95, got %d", internal.MaxHeat)
	}
}

func TestBuildTree_SortsByHeat(t *testing.T) {
	root := BuildTree(makeFiles())

	// First child should be the one with highest max heat
	if len(root.Children) == 0 {
		t.Fatal("root has no children")
	}

	// internal/ has max heat 95, should be first
	if root.Children[0].Name != "internal" {
		t.Errorf("first child should be 'internal' (highest heat), got %q", root.Children[0].Name)
	}
}

func TestFlatten_RootExpandedOnly(t *testing.T) {
	root := BuildTree(makeFiles())

	// Only root is expanded by default
	nodes := Flatten(root)

	// Should show top-level dirs only (cmd, internal, pkg)
	if len(nodes) != 3 {
		t.Errorf("expected 3 visible nodes (top-level dirs), got %d", len(nodes))
	}
}

func TestFlatten_ExpandDirectory(t *testing.T) {
	root := BuildTree(makeFiles())

	// Expand internal/
	for _, c := range root.Children {
		if c.Name == "internal" {
			c.Expanded = true
			break
		}
	}

	nodes := Flatten(root)

	// Should show: cmd, internal, auth, api, pkg (5 nodes)
	if len(nodes) != 5 {
		t.Errorf("expected 5 visible nodes, got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s (dir=%v)", n.Path, n.IsDir)
		}
	}
}

func TestFlatten_ExpandAll(t *testing.T) {
	root := BuildTree(makeFiles())

	// Expand everything
	var expandAll func(*TreeNode)
	expandAll = func(n *TreeNode) {
		n.Expanded = true
		for _, c := range n.Children {
			expandAll(c)
		}
	}
	expandAll(root)

	nodes := Flatten(root)

	// Should show all dirs + all files
	// Dirs: cmd, internal, internal/auth, internal/api, pkg, pkg/utils = 6
	// Files: main.go, jwt.go, session.go, handler.go, format.go = 5
	if len(nodes) != 11 {
		t.Errorf("expected 11 visible nodes, got %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  %s (dir=%v)", n.Path, n.IsDir)
		}
	}
}

func TestFilterByTier_CriticalOnly(t *testing.T) {
	root := BuildTree(makeFiles())

	filtered := FilterByTier(root, []types.Tier{types.TierCritical})
	if filtered == nil {
		t.Fatal("filtered tree should not be nil")
	}

	// Expand all to count
	var expandAll func(*TreeNode)
	expandAll = func(n *TreeNode) {
		n.Expanded = true
		for _, c := range n.Children {
			expandAll(c)
		}
	}
	expandAll(filtered)

	nodes := Flatten(filtered)

	// Should only contain jwt.go and its parent dirs
	var fileCount int
	for _, n := range nodes {
		if !n.IsDir {
			fileCount++
			if n.Heat.Tier != types.TierCritical {
				t.Errorf("non-critical file in filtered tree: %s (tier=%s)", n.Path, n.Heat.Tier)
			}
		}
	}

	if fileCount != 1 {
		t.Errorf("expected 1 critical file, got %d", fileCount)
	}
}

func TestFilterByTier_NoMatch(t *testing.T) {
	files := map[string]*types.FileHeat{
		"safe.go": {Path: "safe.go", HeatScore: 5, Tier: types.TierLow},
	}
	root := BuildTree(files)

	filtered := FilterByTier(root, []types.Tier{types.TierCritical})
	if filtered != nil {
		t.Error("filtered tree should be nil when no matches")
	}
}

func TestSortBy_Name(t *testing.T) {
	root := BuildTree(makeFiles())
	SortBy(root, SortByName)

	// First dir should be alphabetically first
	if root.Children[0].Name != "cmd" {
		t.Errorf("first child sorted by name should be 'cmd', got %q", root.Children[0].Name)
	}
}

func TestSortBy_Heat(t *testing.T) {
	root := BuildTree(makeFiles())
	SortBy(root, SortByHeat)

	// First dir should be the one with highest max heat (internal, 95)
	if root.Children[0].Name != "internal" {
		t.Errorf("first child sorted by heat should be 'internal', got %q", root.Children[0].Name)
	}
}

func TestSearch_FindsByName(t *testing.T) {
	root := BuildTree(makeFiles())

	results := Search(root, "jwt")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'jwt', got %d", len(results))
	}
	if len(results) > 0 && results[0].Name != "jwt.go" {
		t.Errorf("expected jwt.go, got %q", results[0].Name)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	root := BuildTree(makeFiles())

	results := Search(root, "JWT")
	if len(results) != 1 {
		t.Errorf("search should be case insensitive, got %d results", len(results))
	}
}

func TestSearch_NoResults(t *testing.T) {
	root := BuildTree(makeFiles())

	results := Search(root, "nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_MatchesDirectories(t *testing.T) {
	root := BuildTree(makeFiles())

	results := Search(root, "auth")
	found := false
	for _, r := range results {
		if r.IsDir && r.Name == "auth" {
			found = true
		}
	}
	if !found {
		t.Error("search should find directory 'auth'")
	}
}

func TestBuildTree_SingleFile(t *testing.T) {
	files := map[string]*types.FileHeat{
		"main.go": {Path: "main.go", HeatScore: 50, Tier: types.TierMedium},
	}

	root := BuildTree(files)

	if len(root.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(root.Children))
	}
	if root.Children[0].Name != "main.go" {
		t.Errorf("expected main.go, got %q", root.Children[0].Name)
	}
	if root.Children[0].IsDir {
		t.Error("main.go should not be a directory")
	}
}

func TestBuildTree_EmptyFiles(t *testing.T) {
	root := BuildTree(map[string]*types.FileHeat{})

	if len(root.Children) != 0 {
		t.Errorf("expected 0 children for empty files, got %d", len(root.Children))
	}
}

func TestNodeDepth(t *testing.T) {
	root := BuildTree(makeFiles())

	// Expand all
	var expandAll func(*TreeNode)
	expandAll = func(n *TreeNode) {
		n.Expanded = true
		for _, c := range n.Children {
			expandAll(c)
		}
	}
	expandAll(root)

	nodes := Flatten(root)

	for _, n := range nodes {
		expectedDepth := len(strings.Split(n.Path, "/"))
		if n.Depth != expectedDepth {
			t.Errorf("%s: expected depth %d, got %d", n.Path, expectedDepth, n.Depth)
		}
	}
}
