package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zanetworker/highstakes/internal/types"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#EE0000")).
			Padding(0, 1)

	treeStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#505050")).
			Padding(0, 1)

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#505050")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#333333")).
			Foreground(lipgloss.Color("#FFFFFF"))

	criticalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	highStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8844"))
	mediumStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	lowStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#44CC44"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	labelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	barFull       = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE0000"))
	barEmpty      = lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
)

// Model is the Bubbletea model for the TUI
type Model struct {
	heatmap    *types.Heatmap
	tree       *TreeNode
	visible    []*TreeNode
	cursor     int
	scrollOff  int
	width      int
	height     int
	sortMode   SortMode
	searching  bool
	searchInput textinput.Model
	filterTier *types.Tier
	quitting   bool
}

// NewModel creates a new TUI model from a heatmap
func NewModel(hm *types.Heatmap) Model {
	tree := BuildTree(hm.Files)

	// Auto-expand root children
	for _, c := range tree.Children {
		c.Expanded = true
	}

	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 50

	m := Model{
		heatmap:     hm,
		tree:        tree,
		visible:     Flatten(tree),
		searchInput: ti,
		sortMode:    SortByHeat,
	}

	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			return m.handleSearchKey(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
		}

	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
			m.ensureVisible()
		}

	case "enter", "right", "l":
		if m.cursor < len(m.visible) {
			node := m.visible[m.cursor]
			if node.IsDir {
				node.Expanded = !node.Expanded
				m.refreshVisible()
			}
		}

	case "left", "h":
		if m.cursor < len(m.visible) {
			node := m.visible[m.cursor]
			if node.IsDir && node.Expanded {
				node.Expanded = false
				m.refreshVisible()
			}
		}

	case "/":
		m.searching = true
		m.searchInput.Focus()
		return m, textinput.Blink

	case "s":
		m.sortMode = (m.sortMode + 1) % 3
		SortBy(m.tree, m.sortMode)
		m.refreshVisible()

	case "f":
		m.cycleFilter()
		m.refreshVisible()

	case "e":
		// Expand/collapse all
		var toggle func(*TreeNode, bool)
		toggle = func(n *TreeNode, expand bool) {
			n.Expanded = expand
			for _, c := range n.Children {
				toggle(c, expand)
			}
		}
		// Check if mostly expanded
		expanded := 0
		for _, n := range m.visible {
			if n.IsDir && n.Expanded {
				expanded++
			}
		}
		toggle(m.tree, expanded < len(m.visible)/2)
		m.refreshVisible()
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.searchInput.SetValue("")
		m.refreshVisible()
		return m, nil

	case "enter":
		query := m.searchInput.Value()
		if query != "" {
			results := Search(m.tree, query)
			m.visible = results
			m.cursor = 0
			m.scrollOff = 0
		}
		m.searching = false
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m *Model) cycleFilter() {
	if m.filterTier == nil {
		t := types.TierCritical
		m.filterTier = &t
	} else {
		switch *m.filterTier {
		case types.TierCritical:
			t := types.TierHigh
			m.filterTier = &t
		case types.TierHigh:
			t := types.TierMedium
			m.filterTier = &t
		case types.TierMedium:
			t := types.TierLow
			m.filterTier = &t
		default:
			m.filterTier = nil
		}
	}
}

func (m *Model) refreshVisible() {
	if m.filterTier != nil {
		// Show all tiers at or above the filter level
		var tiers []types.Tier
		switch *m.filterTier {
		case types.TierCritical:
			tiers = []types.Tier{types.TierCritical}
		case types.TierHigh:
			tiers = []types.Tier{types.TierCritical, types.TierHigh}
		case types.TierMedium:
			tiers = []types.Tier{types.TierCritical, types.TierHigh, types.TierMedium}
		case types.TierLow:
			tiers = []types.Tier{types.TierCritical, types.TierHigh, types.TierMedium, types.TierLow}
		}
		filtered := FilterByTier(m.tree, tiers)
		if filtered != nil {
			m.visible = Flatten(filtered)
		} else {
			m.visible = []*TreeNode{}
		}
	} else {
		m.visible = Flatten(m.tree)
	}

	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
}

func (m *Model) ensureVisible() {
	treeHeight := m.treeHeight()
	if m.cursor < m.scrollOff {
		m.scrollOff = m.cursor
	}
	if m.cursor >= m.scrollOff+treeHeight {
		m.scrollOff = m.cursor - treeHeight + 1
	}
}

func (m Model) treeHeight() int {
	h := m.height - 6 // Title + help + borders
	if h < 5 {
		h = 5
	}
	return h
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.width == 0 {
		return "Loading..."
	}

	// Layout: title + tree|detail + help
	title := titleStyle.Render(" 🔥 Code Heatmap ")
	title += fmt.Sprintf("  %d files  %s", m.heatmap.Metadata.TotalFiles, m.heatmap.Metadata.Branch)

	treeWidth := m.width*2/5 - 4
	detailWidth := m.width*3/5 - 4
	treeHeight := m.treeHeight()

	// Render tree panel
	treeContent := m.renderTree(treeWidth, treeHeight)
	treePanel := treeStyle.Width(treeWidth).Height(treeHeight).Render(treeContent)

	// Render detail panel
	detailContent := m.renderDetail(detailWidth, treeHeight)
	detailPanel := detailStyle.Width(detailWidth).Height(treeHeight).Render(detailContent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, treePanel, detailPanel)

	// Help bar
	help := m.renderHelp()

	// Search bar (if active)
	if m.searching {
		help = fmt.Sprintf("Search: %s", m.searchInput.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, body, help)
}

func (m Model) renderTree(width, height int) string {
	if len(m.visible) == 0 {
		return dimStyle.Render("No files match filter")
	}

	var lines []string

	end := m.scrollOff + height
	if end > len(m.visible) {
		end = len(m.visible)
	}

	for i := m.scrollOff; i < end; i++ {
		node := m.visible[i]
		line := m.renderTreeLine(node, i == m.cursor, width)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderTreeLine(node *TreeNode, selected bool, width int) string {
	indent := strings.Repeat("  ", node.Depth-1)

	var icon, name, score string

	if node.IsDir {
		if node.Expanded {
			icon = "▼ "
		} else {
			icon = "▶ "
		}
		name = node.Name + "/"
		score = tierIcon(tierFromScore(node.MaxHeat))
	} else {
		icon = "  "
		name = node.Name
		if node.Heat != nil {
			score = fmt.Sprintf("%s %d", tierIcon(node.Heat.Tier), node.Heat.HeatScore)
		}
	}

	// Truncate name if needed
	maxName := width - len(indent) - len(icon) - len(score) - 2
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	padding := width - len(indent) - len(icon) - len(name) - len(score)
	if padding < 1 {
		padding = 1
	}

	line := indent + icon + name + strings.Repeat(" ", padding) + score

	if selected {
		return selectedStyle.Width(width).Render(line)
	}

	return line
}

func (m Model) renderDetail(width, height int) string {
	if m.cursor >= len(m.visible) || len(m.visible) == 0 {
		return dimStyle.Render("Select a file to see details")
	}

	node := m.visible[m.cursor]

	if node.IsDir {
		return m.renderDirDetail(node, width)
	}

	if node.Heat == nil {
		return dimStyle.Render("No heat data")
	}

	return m.renderFileDetail(node.Heat, width)
}

func (m Model) renderDirDetail(node *TreeNode, width int) string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render(node.Path+"/"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Max Heat: "))
	b.WriteString(tierStyle(tierFromScore(node.MaxHeat)).Render(
		fmt.Sprintf("%d %s", node.MaxHeat, tierLabel(tierFromScore(node.MaxHeat)))))
	b.WriteString("\n\n")

	// Count children by tier
	counts := map[types.Tier]int{}
	var countFiles func(*TreeNode)
	countFiles = func(n *TreeNode) {
		if !n.IsDir && n.Heat != nil {
			counts[n.Heat.Tier]++
		}
		for _, c := range n.Children {
			countFiles(c)
		}
	}
	countFiles(node)

	b.WriteString(labelStyle.Render("Files by Tier:"))
	b.WriteString("\n")
	if c := counts[types.TierCritical]; c > 0 {
		b.WriteString(criticalStyle.Render(fmt.Sprintf("  🔥🔥🔥 CRITICAL: %d\n", c)))
	}
	if c := counts[types.TierHigh]; c > 0 {
		b.WriteString(highStyle.Render(fmt.Sprintf("  🔥🔥  HIGH:     %d\n", c)))
	}
	if c := counts[types.TierMedium]; c > 0 {
		b.WriteString(mediumStyle.Render(fmt.Sprintf("  🔥   MEDIUM:   %d\n", c)))
	}
	if c := counts[types.TierLow]; c > 0 {
		b.WriteString(lowStyle.Render(fmt.Sprintf("  🟢   LOW:      %d\n", c)))
	}

	return b.String()
}

func (m Model) renderFileDetail(heat *types.FileHeat, width int) string {
	var b strings.Builder

	// File path
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(heat.Path))
	b.WriteString("\n\n")

	// Heat score with bar
	b.WriteString(labelStyle.Render("Heat Score: "))
	b.WriteString(tierStyle(heat.Tier).Render(
		fmt.Sprintf("%d %s %s", heat.HeatScore, tierIcon(heat.Tier), tierLabel(heat.Tier))))
	b.WriteString("\n")
	b.WriteString(renderBar(heat.HeatScore, width-4))
	b.WriteString("\n\n")

	// Risk factors
	b.WriteString(labelStyle.Render("Risk Factors:"))
	b.WriteString("\n")

	factors := []struct {
		name  string
		score int
		detail string
	}{
		{"Dependency", int(heat.Factors.DependencyCentrality.Score * 100),
			fmt.Sprintf("%d imports", heat.Factors.DependencyCentrality.ImportCount)},
		{"Incidents", heat.Factors.IncidentHistory.Score,
			fmt.Sprintf("%d total", heat.Factors.IncidentHistory.IncidentCount)},
		{"Churn", heat.Factors.ChangeFrequency.Score,
			fmt.Sprintf("%d/90d", heat.Factors.ChangeFrequency.CommitsLast90d)},
		{"User Impact", heat.Factors.UserImpact.Score, ""},
		{"Sensitivity", heat.Factors.DataSensitivity.Score, ""},
		{"Coverage Risk", heat.Factors.TestCoverage.Score,
			fmt.Sprintf("%.0f%%", heat.Factors.TestCoverage.CoveragePercent)},
		{"Complexity", heat.Factors.Complexity.Score,
			fmt.Sprintf("cyclo: %d", heat.Factors.Complexity.Cyclomatic)},
	}

	barWidth := width - 24
	if barWidth < 10 {
		barWidth = 10
	}

	for _, f := range factors {
		label := fmt.Sprintf("  %-14s", f.name)
		b.WriteString(labelStyle.Render(label))
		b.WriteString(renderBar(f.score, barWidth))
		if f.detail != "" {
			b.WriteString(dimStyle.Render(" " + f.detail))
		}
		b.WriteString("\n")
	}

	// Review requirements
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Review:"))
	b.WriteString("\n")

	req := heat.ReviewRequirements
	b.WriteString(fmt.Sprintf("  Reviewers: %d", req.MinReviewers))
	if req.RequiresSenior {
		b.WriteString(" (senior)")
	}
	b.WriteString("\n")
	if req.RequiresSecurityScan {
		b.WriteString("  Security Scan: Required\n")
	}
	if req.AutoMerge {
		b.WriteString(lowStyle.Render("  Auto-Merge: ✅ Allowed\n"))
	} else {
		b.WriteString(criticalStyle.Render("  Auto-Merge: ❌ Blocked\n"))
	}

	// Recent changes
	if len(heat.RecentChanges) > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Recent Changes:"))
		b.WriteString("\n")
		limit := 5
		if len(heat.RecentChanges) < limit {
			limit = len(heat.RecentChanges)
		}
		for _, ch := range heat.RecentChanges[:limit] {
			date := ch.Date.Format("2006-01-02")
			msg := ch.Message
			if len(msg) > width-16 {
				msg = msg[:width-19] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s  %s\n", dimStyle.Render(date), msg))
		}
	}

	return b.String()
}

func (m Model) renderHelp() string {
	sortLabel := "heat"
	switch m.sortMode {
	case SortByName:
		sortLabel = "name"
	case SortBySize:
		sortLabel = "size"
	}

	filterLabel := "all"
	if m.filterTier != nil {
		filterLabel = string(*m.filterTier) + "+"
	}

	parts := []string{
		"↑↓ navigate",
		"enter expand",
		fmt.Sprintf("[s]ort:%s", sortLabel),
		fmt.Sprintf("[f]ilter:%s", filterLabel),
		"[e]xpand all",
		"[/]search",
		"[q]uit",
	}

	return helpStyle.Render(strings.Join(parts, "  "))
}

func renderBar(score, width int) string {
	if width < 1 {
		width = 1
	}
	filled := score * width / 100
	empty := width - filled
	if empty < 0 {
		empty = 0
	}

	return barFull.Render(strings.Repeat("█", filled)) +
		barEmpty.Render(strings.Repeat("░", empty))
}

func tierFromScore(score int) types.Tier {
	switch {
	case score >= 86:
		return types.TierCritical
	case score >= 61:
		return types.TierHigh
	case score >= 31:
		return types.TierMedium
	default:
		return types.TierLow
	}
}

func tierIcon(tier types.Tier) string {
	switch tier {
	case types.TierCritical:
		return "🔥🔥🔥"
	case types.TierHigh:
		return "🔥🔥"
	case types.TierMedium:
		return "🔥"
	default:
		return "🟢"
	}
}

func tierLabel(tier types.Tier) string {
	return strings.ToUpper(string(tier))
}

func tierStyle(tier types.Tier) lipgloss.Style {
	switch tier {
	case types.TierCritical:
		return criticalStyle
	case types.TierHigh:
		return highStyle
	case types.TierMedium:
		return mediumStyle
	default:
		return lowStyle
	}
}
