package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mhutchinson/woodpecker/model"
	"github.com/sahilm/fuzzy"
	distclient "github.com/transparency-dev/distributor/client"
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

// logItem wraps log origins for use in bubbles/list.
type logItem struct {
	origin  string
	logType string
	url     string
}

func (i logItem) Title() string       { return i.origin }
func (i logItem) Description() string { return fmt.Sprintf("Type: %s | URL: %s", i.logType, i.url) }
func (i logItem) FilterValue() string {
	return fmt.Sprintf("%s\x01%s\x01%s", i.origin, i.logType, i.url)
}

// Messages for the Bubble Tea loop.
type tickMsg struct{}

type checkpointMsg struct {
	checkpoint *model.Checkpoint
	witnessed  *model.Checkpoint
	err        error
}

type leafMsg struct {
	leaf model.Leaf
	err  error
}

type distributorClient interface {
	GetCheckpointN(l distclient.LogID, n uint) ([]byte, error)
}

// Model represents the state of our Bubble Tea TUI.
type Model struct {
	logOrigins    []string
	logClients    map[string]logClient
	distributor   distributorClient
	witVerifiers  []note.Verifier
	currentLog    string
	currentClient logClient

	// App state
	checkpoint *model.Checkpoint
	witnessed  *model.Checkpoint
	witnessN   uint
	leaf       model.Leaf
	activeErr  error

	// Sub-components
	list      list.Model
	textInput textinput.Model
	spinner   spinner.Model
	viewport  viewport.Model

	// UI layout state
	activeView   string // "leaf", "logs", "jump"
	width        int
	height       int
	loadingCheck bool
	loadingLeaf  bool
}

func NewModel(origins []string, clients map[string]logClient, dist distributorClient, witVers []note.Verifier, initialLog string) *Model {
	items := make([]list.Item, len(origins))
	for i, o := range origins {
		client, ok := clients[o]
		logType := "unknown"
		urlStr := "unknown"
		if ok && client != nil {
			logType = client.GetLogType()
			urlStr = client.GetURL()
		}
		items[i] = logItem{
			origin:  o,
			logType: logType,
			url:     urlStr,
		}
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select Transparency Log"
	l.SetShowHelp(false)
	l.Filter = fuzzyFilter

	ti := textinput.New()
	ti.Placeholder = "Leaf Index (e.g. 1234)"
	ti.CharLimit = 20
	ti.Width = 20

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#14B8A6"))

	vp := viewport.New(0, 0)

	m := &Model{
		logOrigins:    origins,
		logClients:    clients,
		distributor:   dist,
		witVerifiers:  witVers,
		currentLog:    initialLog,
		currentClient: clients[initialLog],
		witnessN:      2,
		list:          l,
		textInput:     ti,
		spinner:       s,
		viewport:      vp,
		activeView:    "leaf",
		loadingCheck:  true,
		loadingLeaf:   true,
	}

	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchCheckpointCmd(),
		m.startPeriodicTicker(),
	)
}

func (m *Model) startPeriodicTicker() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m *Model) selectLog(origin string) {
	if client, ok := m.logClients[origin]; ok {
		m.currentLog = origin
		m.currentClient = client
		m.checkpoint = nil
		m.witnessed = nil
		m.leaf = model.Leaf{}
		m.activeErr = nil
		m.loadingCheck = true
		m.loadingLeaf = true
	}
}

func (m *Model) fetchCheckpointCmd() tea.Cmd {
	return func() tea.Msg {
		witnessed := make(chan *model.Checkpoint, 1)
		go func() {
			defer close(witnessed)
			logID := distclient.LogID(log.ID(m.currentClient.GetOrigin()))
			bs, err := m.distributor.GetCheckpointN(logID, m.witnessN)
			if err != nil {
				witnessed <- nil
				return
			}
			cp, _, n, err := log.ParseCheckpoint(bs, m.currentClient.GetOrigin(), m.currentClient.GetVerifier(), m.witVerifiers...)
			if err != nil {
				witnessed <- nil
				return
			}
			witnessed <- &model.Checkpoint{
				Checkpoint: cp,
				Note:       n,
				Raw:        bs,
			}
		}()

		cp, err := m.currentClient.GetCheckpoint()
		wCP := <-witnessed

		return checkpointMsg{
			checkpoint: cp,
			witnessed:  wCP,
			err:        err,
		}
	}
}

func (m *Model) fetchLeafCmd(index uint64) tea.Cmd {
	return func() tea.Msg {
		if m.checkpoint == nil || m.checkpoint.Size == 0 {
			return leafMsg{err: fmt.Errorf("no checkpoint loaded")}
		}
		if index >= m.checkpoint.Size {
			return leafMsg{err: fmt.Errorf("cannot fetch leaf bigger than checkpoint size %d", m.checkpoint.Size)}
		}
		leafBytes, err := m.currentClient.GetLeaf(m.checkpoint.Size, index)
		return leafMsg{
			leaf: model.Leaf{
				Contents: leafBytes,
				Index:    index,
			},
			err: err,
		}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Global quit key
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// 1. View-specific updates
	switch m.activeView {
	case "logs":
		// Key interception when not filtering
		if keyMsg, ok := msg.(tea.KeyMsg); ok && m.list.FilterState() != list.Filtering {
			switch keyMsg.String() {
			case "enter":
				if selected, ok := m.list.SelectedItem().(logItem); ok {
					m.selectLog(selected.origin)
					m.activeView = "leaf"
					return m, m.fetchCheckpointCmd()
				}
			case "esc", "q":
				m.activeView = "leaf"
				return m, nil
			}
		}

		// Forward all messages to the list when in logs view
		var listCmd tea.Cmd
		m.list, listCmd = m.list.Update(msg)
		if listCmd != nil {
			cmds = append(cmds, listCmd)
		}

	case "jump":
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				val := m.textInput.Value()
				if idx, err := strconv.ParseUint(val, 10, 64); err == nil && m.checkpoint != nil {
					if idx < m.checkpoint.Size {
						m.loadingLeaf = true
						m.activeView = "leaf"
						return m, m.fetchLeafCmd(idx)
					}
				}
				m.activeView = "leaf"
				return m, nil
			case "esc":
				m.activeView = "leaf"
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case "leaf":
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "q":
				return m, tea.Quit
			case "left":
				if m.leaf.Index > 0 && m.checkpoint != nil {
					m.loadingLeaf = true
					return m, m.fetchLeafCmd(m.leaf.Index - 1)
				}
			case "right":
				if m.checkpoint != nil && m.leaf.Index+1 < m.checkpoint.Size {
					m.loadingLeaf = true
					return m, m.fetchLeafCmd(m.leaf.Index + 1)
				}
			case "l":
				m.activeView = "logs"
				return m, nil
			case "g":
				m.activeView = "jump"
				m.textInput.Reset()
				m.textInput.Focus()
				return m, textinput.Blink
			case "w":
				m.witnessN++
				m.loadingCheck = true
				return m, m.fetchCheckpointCmd()
			case "W":
				if m.witnessN > 1 {
					m.witnessN--
					m.loadingCheck = true
					return m, m.fetchCheckpointCmd()
				}
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// 2. Global message handling
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		checkpointHeight := 8
		if m.height < 25 {
			checkpointHeight = 5
		}

		viewportHeight := m.height - checkpointHeight - 10
		if viewportHeight < 5 {
			viewportHeight = 5
		}

		vpHeight := viewportHeight - 2
		if vpHeight < 3 {
			vpHeight = 3
		}

		m.viewport.Width = m.width - 6
		m.viewport.Height = vpHeight
		m.list.SetSize(msg.Width-6, vpHeight)

	case tickMsg:
		return m, tea.Batch(append(cmds, m.fetchCheckpointCmd(), m.startPeriodicTicker())...)

	case checkpointMsg:
		m.loadingCheck = false
		m.activeErr = msg.err
		if msg.err == nil {
			m.checkpoint = msg.checkpoint
			m.witnessed = msg.witnessed
			// Load the last leaf if none is loaded or index is out of bounds
			if m.leaf.Contents == nil || (m.checkpoint != nil && m.leaf.Index >= m.checkpoint.Size) {
				if m.checkpoint != nil && m.checkpoint.Size > 0 {
					m.loadingLeaf = true
					return m, tea.Batch(append(cmds, m.fetchLeafCmd(m.checkpoint.Size-1))...)
				}
			}
		}

	case leafMsg:
		m.loadingLeaf = false
		m.activeErr = msg.err
		if msg.err == nil {
			m.leaf = msg.leaf
			m.viewport.SetContent(m.currentClient.FormatLeaf(msg.leaf.Contents))
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing Woodpecker TUI..."
	}

	var sb strings.Builder

	// Header Title Bar (Height: 1 line, no padding)
	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#4F46E5")). // Deep Purple background
		Foreground(lipgloss.Color("#FFFFFF")). // Bold White text
		Bold(true).
		Align(lipgloss.Center).
		Width(m.width - 2)

	sb.WriteString(headerStyle.Render(" WOODPECKER LOG INSPECTOR "))
	sb.WriteString("\n\n") // Space: 1 line

	// Checkpoints Panel
	colWidth := (m.width - 6) / 2
	if colWidth < 20 {
		colWidth = 20
	}

	checkpointHeight := 8
	if m.height < 25 {
		checkpointHeight = 5
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#6366F1")).
		Padding(0, 2). // No vertical padding to conserve screen space
		Width(colWidth).
		Height(checkpointHeight)

	accentPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#14B8A6")).
		Padding(0, 2). // No vertical padding to conserve screen space
		Width(colWidth).
		Height(checkpointHeight)

	var cpText string
	if m.loadingCheck {
		cpText = fmt.Sprintf("%s Fetching checkpoint...", m.spinner.View())
	} else if m.checkpoint != nil {
		cpText = string(m.checkpoint.Raw)
	} else if m.activeErr != nil {
		cpText = fmt.Sprintf("Error: %v", m.activeErr)
	} else {
		cpText = "No checkpoint data."
	}

	maxContentLines := checkpointHeight - 2
	if maxContentLines < 1 {
		maxContentLines = 1
	}

	// Truncate cpText to fit inside maxContentLines to avoid visual clipping
	cpLines := strings.Split(cpText, "\n")
	if len(cpLines) > maxContentLines {
		cpText = strings.Join(cpLines[:maxContentLines], "\n")
	}

	var witnessedText string
	if m.loadingCheck {
		witnessedText = fmt.Sprintf("%s Fetching witnessed checkpoint...", m.spinner.View())
	} else if m.witnessed != nil {
		var wsb strings.Builder
		fmt.Fprintf(&wsb, "Size: %d\nHash: %x\n", m.witnessed.Size, m.witnessed.Hash)
		if len(m.witnessed.Note.Sigs) > 1 {
			wsb.WriteString("\nWitnesses:\n")
			for _, w := range m.witnessed.Note.Sigs[1:] {
				wsb.WriteString(" • " + w.Name + "\n")
			}
		}
		witnessedText = wsb.String()
	} else {
		witnessedText = "No witnessed signatures found at this level."
	}

	// Truncate witnessedText to fit inside maxContentLines to avoid visual clipping
	witLines := strings.Split(witnessedText, "\n")
	if len(witLines) > maxContentLines {
		witnessedText = strings.Join(witLines[:maxContentLines], "\n")
	}

	leftPanel := panelStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A78BFA")).Render(fmt.Sprintf("Checkpoint: %s", m.currentLog)),
			"",
			cpText,
		),
	)

	rightPanel := accentPanelStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2DD4BF")).Render(fmt.Sprintf("Witnessed Checkpoint (N=%d)", m.witnessN)),
			"",
			witnessedText,
		),
	)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel))
	sb.WriteString("\n\n") // Space: 1 line

	// Lower Panel (dynamic depending on view)
	viewportHeight := m.height - checkpointHeight - 10
	if viewportHeight < 5 {
		viewportHeight = 5
	}

	mainBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#A78BFA")).
		Padding(0, 2). // No vertical padding to conserve screen space
		Width(m.width - 2).
		Height(viewportHeight)

	switch m.activeView {
	case "logs":
		sb.WriteString(mainBoxStyle.BorderForeground(lipgloss.Color("#4F46E5")).Render(m.list.View()))
	case "jump":
		sb.WriteString(mainBoxStyle.BorderForeground(lipgloss.Color("#14B8A6")).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2DD4BF")).Render("Jump to Leaf Index"),
				"",
				m.textInput.View(),
				"",
				lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#6B7280")).Render("Press Enter to jump, Escape to cancel"),
			),
		))
	default:
		var leafTitle string
		if m.loadingLeaf {
			leafTitle = fmt.Sprintf("Leaf %d (Fetching...)", m.leaf.Index)
		} else {
			leafTitle = fmt.Sprintf("Leaf %d", m.leaf.Index)
		}

		sb.WriteString(mainBoxStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C084FC")).Render(leafTitle),
				"",
				m.viewport.View(),
			),
		))
	}
	sb.WriteString("\n\n") // Space: 1 line

	// Footer Help Bar (Height: 1 line)
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Italic(true)

	sb.WriteString(footerStyle.Render(" [q] Quit  •  [←/→] Prev/Next Leaf  •  [↑/↓] Scroll Content  •  [l] Switch Log  •  [g] Jump  •  [w/W] Witnesses"))

	return sb.String()
}

func mergeIndices(existing, newIndices []int) []int {
	m := make(map[int]bool)
	for _, idx := range existing {
		m[idx] = true
	}
	for _, idx := range newIndices {
		m[idx] = true
	}
	result := make([]int, 0, len(m))
	for idx := range m {
		result = append(result, idx)
	}
	sort.Ints(result)
	return result
}

type scoreRank struct {
	list.Rank
	score int
}

func fuzzyFilter(query string, targets []string) []list.Rank {

	if query == "" {
		ranks := make([]list.Rank, len(targets))
		for i := range targets {
			ranks[i] = list.Rank{
				Index: i,
			}
		}
		return ranks
	}

	terms := strings.Fields(query)
	if len(terms) == 0 {
		ranks := make([]list.Rank, len(targets))
		for i := range targets {
			ranks[i] = list.Rank{
				Index: i,
			}
		}
		return ranks
	}

	matchedTargets := make(map[int]map[int]fuzzy.Match)
	for termIdx, term := range terms {
		matches := fuzzy.FindNoSort(term, targets)
		for _, m := range matches {
			if matchedTargets[m.Index] == nil {
				matchedTargets[m.Index] = make(map[int]fuzzy.Match)
			}
			matchedTargets[m.Index][termIdx] = m
		}
	}

	var sRanks []scoreRank
	for targetIdx, termMatches := range matchedTargets {
		if len(termMatches) != len(terms) {
			continue
		}

		var score int
		var matchedIndices []int
		for _, m := range termMatches {
			score += m.Score
			matchedIndices = mergeIndices(matchedIndices, m.MatchedIndexes)
		}

		targetVal := targets[targetIdx]
		limit := strings.IndexByte(targetVal, 0x01)
		if limit != -1 {
			var filtered []int
			for _, idx := range matchedIndices {
				if idx < limit {
					filtered = append(filtered, idx)
				}
			}
			matchedIndices = filtered
		}

		sRanks = append(sRanks, scoreRank{
			Rank: list.Rank{
				Index:          targetIdx,
				MatchedIndexes: matchedIndices,
			},
			score: score,
		})
	}

	sort.SliceStable(sRanks, func(i, j int) bool {
		return sRanks[i].score > sRanks[j].score
	})

	ranks := make([]list.Rank, len(sRanks))
	for i, sr := range sRanks {
		ranks[i] = sr.Rank
	}
	return ranks
}
