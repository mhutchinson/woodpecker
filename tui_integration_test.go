package main

import (
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mhutchinson/woodpecker/model"
	distclient "github.com/transparency-dev/distributor/client"
	"golang.org/x/mod/sumdb/note"
)

type customMockClient struct {
	origin  string
	logType string
	url     string
}

func (c *customMockClient) GetOrigin() string                          { return c.origin }
func (c *customMockClient) GetVerifier() note.Verifier                 { return nil }
func (c *customMockClient) GetCheckpoint() (*model.Checkpoint, error)  { return nil, nil }
func (c *customMockClient) GetLeaf(size, index uint64) ([]byte, error) { return nil, nil }
func (c *customMockClient) FormatLeaf(leaf []byte) string              { return "" }
func (c *customMockClient) GetLogType() string                         { return c.logType }
func (c *customMockClient) GetURL() string                             { return c.url }

type mockDistributor struct {
	checkpoint []byte
	err        error
}

func (m *mockDistributor) GetCheckpointN(l distclient.LogID, n uint) ([]byte, error) {
	if m.checkpoint == nil {
		return nil, errors.New("mock distributor: no checkpoint configured")
	}
	return m.checkpoint, m.err
}

func runCmdWithTimeout(cmd tea.Cmd, timeout time.Duration) (tea.Msg, bool) {
	ch := make(chan tea.Msg, 1)
	go func() {
		ch <- cmd()
	}()
	select {
	case msg := <-ch:
		return msg, true
	case <-time.After(timeout):
		return nil, false
	}
}

func processCmds(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	var queue []tea.Cmd
	if cmd != nil {
		queue = append(queue, cmd)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		msg, ok := runCmdWithTimeout(curr, 20*time.Millisecond)
		if !ok {
			continue // skip blocking commands like tickers
		}
		if msg == nil {
			continue
		}

		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub != nil {
					queue = append(queue, sub)
				}
			}
			continue
		}

		var nextCmd tea.Cmd
		var newModel tea.Model
		newModel, nextCmd = m.Update(msg)
		*m = *(newModel.(*Model))

		if nextCmd != nil {
			queue = append(queue, nextCmd)
		}
	}
}

func sendKey(t *testing.T, m *Model, keyMsg tea.KeyMsg) {
	t.Helper()
	newM, cmd := m.Update(keyMsg)
	*m = *(newM.(*Model))
	processCmds(t, m, cmd)
}

func sendString(t *testing.T, m *Model, str string) {
	t.Helper()
	for _, r := range str {
		sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTUIFilteringIntegration(t *testing.T) {
	mockClients := []customMockClient{
		{origin: "go.sum database tree", logType: "sumdb", url: "https://sum.golang.org/"},
		{origin: "log2025-1.rekor.sigstore.dev", logType: "tiles", url: "https://log2025-1.rekor.sigstore.dev/api/v2/"},
		{origin: "transparency.dev/armored-witness/firmware_transparency/prod/1", logType: "serverless", url: "https://api.transparency.dev/armored-witness-firmware/prod/log/1/"},
		{origin: "transparency.dev/armored-witness/firmware_transparency/ci/4", logType: "serverless", url: "https://api.transparency.dev/armored-witness-firmware/ci/4/"},
		{origin: "Armory Drive Prod 2", logType: "serverless", url: "https://raw.githubusercontent.com/f-secure-foundry/armory-drive-log/master/log/"},
		{origin: "coachandhorses2026h1.staging.certificate.transparency.goog", logType: "static-ct", url: "https://storage.googleapis.com/coachandhorses2026h1.staging.certificate.transparency.goog/"},
	}

	clientsMap := make(map[string]logClient)
	var origins []string
	for _, c := range mockClients {
		cc := c // local copy
		clientsMap[c.origin] = &cc
		origins = append(origins, c.origin)
	}

	m := NewModel(origins, clientsMap, &mockDistributor{}, nil, "go.sum database tree")
	m.activeView = "leaf"

	// 1. Open the log picker view ('l')
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.activeView != "logs" {
		t.Fatalf("expected activeView to be 'logs', got %s", m.activeView)
	}

	// Verify all items are initially visible
	initialVisible := len(m.list.VisibleItems())
	if initialVisible != len(mockClients) {
		t.Fatalf("expected %d visible items, got %d", len(mockClients), initialVisible)
	}

	// 2. Enter filter mode ('/')
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("expected list filter state to be Filtering, got %v", m.list.FilterState())
	}

	// 3. Type "static-ct" to filter
	sendString(t, m, "static-ct")

	// Verify it filtered down to only coachandhorses2026h1
	visible := m.list.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filtering for 'static-ct', got %d", len(visible))
	}
	selected, ok := visible[0].(logItem)
	if !ok {
		t.Fatalf("visible item is not a logItem")
	}
	if selected.origin != "coachandhorses2026h1.staging.certificate.transparency.goog" {
		t.Errorf("expected filtered item to be coachandhorses2026h1, got %s", selected.origin)
	}

	// 4. Backspace to clear filter, then type "nonexistent"
	// Let's just reset the query by sending backspaces, or we can send esc and press '/' again.
	// Actually, we can send backspaces. "static-ct" has 9 characters.
	for i := 0; i < 9; i++ {
		sendKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}

	visibleAfterClear := len(m.list.VisibleItems())
	if visibleAfterClear != len(mockClients) {
		t.Fatalf("expected %d visible items after clearing filter, got %d", len(mockClients), visibleAfterClear)
	}

	// Type "nonexistent"
	sendString(t, m, "nonexistent")

	visibleNone := len(m.list.VisibleItems())
	if visibleNone != 0 {
		t.Errorf("expected 0 visible items after filtering for 'nonexistent', got %d", visibleNone)
	}
}

func TestTUIFilteringAndSelectionIntegration(t *testing.T) {
	mockClients := []customMockClient{
		{origin: "go.sum database tree", logType: "sumdb", url: "https://sum.golang.org/"},
		{origin: "log2025-1.rekor.sigstore.dev", logType: "tiles", url: "https://log2025-1.rekor.sigstore.dev/api/v2/"},
		{origin: "transparency.dev/armored-witness/firmware_transparency/prod/1", logType: "serverless", url: "https://api.transparency.dev/armored-witness-firmware/prod/log/1/"},
		{origin: "transparency.dev/armored-witness/firmware_transparency/ci/4", logType: "serverless", url: "https://api.transparency.dev/armored-witness-firmware/ci/4/"},
		{origin: "Armory Drive Prod 2", logType: "serverless", url: "https://raw.githubusercontent.com/f-secure-foundry/armory-drive-log/master/log/"},
		{origin: "coachandhorses2026h1.staging.certificate.transparency.goog", logType: "static-ct", url: "https://storage.googleapis.com/coachandhorses2026h1.staging.certificate.transparency.goog/"},
	}

	clientsMap := make(map[string]logClient)
	var origins []string
	for _, c := range mockClients {
		cc := c
		clientsMap[c.origin] = &cc
		origins = append(origins, c.origin)
	}

	m := NewModel(origins, clientsMap, &mockDistributor{}, nil, "go.sum database tree")
	m.activeView = "leaf"

	// 1. Open log picker
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	// 2. Enter filter mode
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	// 3. Type "static-ct"
	sendString(t, m, "static-ct")

	// 4. Press Enter to commit filter
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.list.FilterState() == list.Filtering {
		t.Fatalf("expected list filter state to NOT be Filtering after Enter, got %v", m.list.FilterState())
	}

	// 5. Press Enter to select
	sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// Verify we are back to leaf view and the log is changed
	if m.activeView != "leaf" {
		t.Errorf("expected activeView to be 'leaf', got %s", m.activeView)
	}
	if m.currentLog != "coachandhorses2026h1.staging.certificate.transparency.goog" {
		t.Errorf("expected currentLog to be coachandhorses2026h1..., got %s", m.currentLog)
	}
}
