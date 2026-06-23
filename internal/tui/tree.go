package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// TreeNode represents a node in the file tree (either a directory or file)
type TreeNode struct {
	Name     string
	Path     string      // Full relative path
	IsDir    bool
	Heat     *types.FileHeat // nil for directories
	Children []*TreeNode
	MaxHeat  int             // Max heat score in subtree (for directories)
	Expanded bool
	Depth    int
}

// BuildTree creates a tree structure from flat file map
func BuildTree(files map[string]*types.FileHeat) *TreeNode {
	root := &TreeNode{
		Name:     ".",
		Path:     ".",
		IsDir:    true,
		Children: []*TreeNode{},
		Expanded: true,
		Depth:    0,
	}

	for path, heat := range files {
		parts := strings.Split(filepath.ToSlash(path), "/")
		insertNode(root, parts, path, heat, 0)
	}

	// Calculate max heat for directories
	calculateMaxHeat(root)

	// Sort children by heat (hottest first)
	sortTree(root)

	return root
}

// insertNode adds a file to the tree, creating intermediate directories
func insertNode(parent *TreeNode, parts []string, fullPath string, heat *types.FileHeat, depth int) {
	if len(parts) == 0 {
		return
	}

	name := parts[0]

	// Find existing child
	var child *TreeNode
	for _, c := range parent.Children {
		if c.Name == name {
			child = c
			break
		}
	}

	if len(parts) == 1 {
		// Leaf node (file)
		if child == nil {
			child = &TreeNode{
				Name:  name,
				Path:  fullPath,
				IsDir: false,
				Heat:  heat,
				Depth: depth + 1,
			}
			parent.Children = append(parent.Children, child)
		}
		return
	}

	// Directory node
	if child == nil {
		dirPath := strings.Join(parts[:1], "/")
		if parent.Path != "." {
			dirPath = parent.Path + "/" + parts[0]
		}
		child = &TreeNode{
			Name:     name,
			Path:     dirPath,
			IsDir:    true,
			Children: []*TreeNode{},
			Expanded: false,
			Depth:    depth + 1,
		}
		parent.Children = append(parent.Children, child)
	}

	insertNode(child, parts[1:], fullPath, heat, depth+1)
}

// calculateMaxHeat sets MaxHeat for each directory node
func calculateMaxHeat(node *TreeNode) int {
	if !node.IsDir {
		if node.Heat != nil {
			return node.Heat.HeatScore
		}
		return 0
	}

	maxHeat := 0
	for _, child := range node.Children {
		childHeat := calculateMaxHeat(child)
		if childHeat > maxHeat {
			maxHeat = childHeat
		}
	}
	node.MaxHeat = maxHeat
	return maxHeat
}

// sortTree sorts children: directories first, then by heat score descending
func sortTree(node *TreeNode) {
	sort.Slice(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]

		// Directories before files
		if a.IsDir != b.IsDir {
			return a.IsDir
		}

		// By heat score descending
		aHeat := a.MaxHeat
		bHeat := b.MaxHeat
		if !a.IsDir && a.Heat != nil {
			aHeat = a.Heat.HeatScore
		}
		if !b.IsDir && b.Heat != nil {
			bHeat = b.Heat.HeatScore
		}

		return aHeat > bHeat
	})

	for _, child := range node.Children {
		if child.IsDir {
			sortTree(child)
		}
	}
}

// Flatten returns visible nodes for rendering (respects Expanded state)
func Flatten(root *TreeNode) []*TreeNode {
	var nodes []*TreeNode
	flattenRecursive(root, &nodes)
	return nodes
}

func flattenRecursive(node *TreeNode, nodes *[]*TreeNode) {
	// Skip root itself but show its children
	if node.Path != "." {
		*nodes = append(*nodes, node)
	}

	if node.IsDir && node.Expanded {
		for _, child := range node.Children {
			flattenRecursive(child, nodes)
		}
	}
}

// FilterByTier returns only nodes matching the given tiers
func FilterByTier(root *TreeNode, tiers []types.Tier) *TreeNode {
	tierSet := make(map[types.Tier]bool)
	for _, t := range tiers {
		tierSet[t] = true
	}
	return filterNode(root, tierSet)
}

func filterNode(node *TreeNode, tiers map[types.Tier]bool) *TreeNode {
	if !node.IsDir {
		if node.Heat != nil && tiers[node.Heat.Tier] {
			return node
		}
		return nil
	}

	// For directories, include if any child matches
	filtered := &TreeNode{
		Name:     node.Name,
		Path:     node.Path,
		IsDir:    true,
		Expanded: node.Expanded,
		Depth:    node.Depth,
		Children: []*TreeNode{},
	}

	for _, child := range node.Children {
		filteredChild := filterNode(child, tiers)
		if filteredChild != nil {
			filtered.Children = append(filtered.Children, filteredChild)
		}
	}

	if len(filtered.Children) == 0 {
		return nil
	}

	calculateMaxHeat(filtered)
	return filtered
}

// SortMode determines how nodes are sorted
type SortMode int

const (
	SortByHeat SortMode = iota
	SortByName
	SortBySize
)

// SortBy re-sorts the tree by the given mode
func SortBy(node *TreeNode, mode SortMode) {
	sort.Slice(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]

		// Directories always first
		if a.IsDir != b.IsDir {
			return a.IsDir
		}

		switch mode {
		case SortByName:
			return a.Name < b.Name
		case SortBySize:
			aSize, bSize := 0, 0
			if a.Heat != nil {
				aSize = a.Heat.Size.Lines
			}
			if b.Heat != nil {
				bSize = b.Heat.Size.Lines
			}
			return aSize > bSize
		default: // SortByHeat
			aHeat := a.MaxHeat
			bHeat := b.MaxHeat
			if !a.IsDir && a.Heat != nil {
				aHeat = a.Heat.HeatScore
			}
			if !b.IsDir && b.Heat != nil {
				bHeat = b.Heat.HeatScore
			}
			return aHeat > bHeat
		}
	})

	for _, child := range node.Children {
		if child.IsDir {
			SortBy(child, mode)
		}
	}
}

// Search returns nodes whose names contain the query
func Search(root *TreeNode, query string) []*TreeNode {
	query = strings.ToLower(query)
	var results []*TreeNode
	searchRecursive(root, query, &results)
	return results
}

func searchRecursive(node *TreeNode, query string, results *[]*TreeNode) {
	if strings.Contains(strings.ToLower(node.Name), query) && node.Path != "." {
		*results = append(*results, node)
	}

	if node.IsDir {
		for _, child := range node.Children {
			searchRecursive(child, query, results)
		}
	}
}
