package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

type mockLogClient struct {
	origin     string
	getLeafErr error
	leafBytes  []byte
}

func (m *mockLogClient) GetOrigin() string {
	if m.origin != "" {
		return m.origin
	}
	return "origin"
}
func (m *mockLogClient) GetVerifier() note.Verifier                { return nil }
func (m *mockLogClient) GetCheckpoint() (*model.Checkpoint, error) { return nil, nil }
func (m *mockLogClient) GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error) {
	if m.getLeafErr != nil {
		return nil, m.getLeafErr
	}
	if m.leafBytes != nil {
		return m.leafBytes, nil
	}
	return []byte("leaf"), nil
}
func (m *mockLogClient) FormatLeaf(leaf []byte) string {
	return "formatted: " + string(leaf)
}
func (m *mockLogClient) GetLogType() string { return "mock" }
func (m *mockLogClient) GetURL() string     { return "http://mock" }

func TestPrevLeafUnderflow(t *testing.T) {
	clients := map[string]logClient{
		"origin": &mockLogClient{},
	}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")

	// Set up model state
	m.checkpoint = &model.Checkpoint{
		Checkpoint: &log.Checkpoint{Size: 10},
	}
	m.leaf = model.Leaf{Index: 0}
	m.activeView = "leaf"

	// Pressing Left should no longer underflow and should NOT return a new fetch Cmd
	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyLeft})

	resM := newM.(*Model)
	if resM.activeErr != nil {
		t.Errorf("Expected no error, but got %v", resM.activeErr)
	}

	if resM.leaf.Index != 0 {
		t.Errorf("Expected leaf index to remain 0, but got %d", resM.leaf.Index)
	}

	if cmd != nil {
		t.Errorf("Expected no command to be returned on underflow, but got one")
	}
}

func TestNextLeafOverflow(t *testing.T) {
	clients := map[string]logClient{
		"origin": &mockLogClient{},
	}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")

	// Set up model state: last leaf is at index 9 for size 10
	m.checkpoint = &model.Checkpoint{
		Checkpoint: &log.Checkpoint{Size: 10},
	}
	m.leaf = model.Leaf{Index: 9}
	m.activeView = "leaf"

	// Pressing Right should no longer go out of bounds and should NOT return a new fetch Cmd
	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})

	resM := newM.(*Model)
	if resM.activeErr != nil {
		t.Errorf("Expected no error, but got %v", resM.activeErr)
	}

	if resM.leaf.Index != 9 {
		t.Errorf("Expected leaf index to remain 9, but got %d", resM.leaf.Index)
	}

	if cmd != nil {
		t.Errorf("Expected no command to be returned on overflow, but got one")
	}
}

func TestFetchLeafCmd(t *testing.T) {
	client := &mockLogClient{leafBytes: []byte("test-leaf-data")}
	clients := map[string]logClient{"origin": client}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")

	// 1. Nil checkpoint
	m.checkpoint = nil
	cmd := m.fetchLeafCmd(0)
	msg := cmd().(leafMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "no checkpoint loaded") {
		t.Errorf("expected 'no checkpoint loaded' error, got %v", msg.err)
	}

	// 2. Size 0 checkpoint
	m.checkpoint = &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 0}}
	cmd = m.fetchLeafCmd(0)
	msg = cmd().(leafMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "no checkpoint loaded") {
		t.Errorf("expected 'no checkpoint loaded' error for size 0, got %v", msg.err)
	}

	// 3. Index >= checkpoint size
	m.checkpoint = &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 5}}
	cmd = m.fetchLeafCmd(5)
	msg = cmd().(leafMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "cannot fetch leaf bigger than checkpoint size") {
		t.Errorf("expected out of bounds error, got %v", msg.err)
	}

	// 4. Successful leaf fetch
	cmd = m.fetchLeafCmd(2)
	msg = cmd().(leafMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error fetching leaf: %v", msg.err)
	}
	if msg.leaf.Index != 2 {
		t.Errorf("expected leaf index 2, got %d", msg.leaf.Index)
	}
	if string(msg.leaf.Contents) != "test-leaf-data" {
		t.Errorf("expected leaf contents 'test-leaf-data', got %q", string(msg.leaf.Contents))
	}

	// 5. Client returns error
	client.getLeafErr = errors.New("network failure")
	cmd = m.fetchLeafCmd(2)
	msg = cmd().(leafMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "network failure") {
		t.Errorf("expected client error to propagate, got %v", msg.err)
	}
}

func TestUpdateNavigation(t *testing.T) {
	client := &mockLogClient{leafBytes: []byte("leaf-bytes")}
	clients := map[string]logClient{"origin": client}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")
	m.checkpoint = &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 10}}
	m.leaf = model.Leaf{Index: 5}
	m.activeView = "leaf"

	// 1. Left arrow decrements leaf index and returns fetch command
	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	resM := newM.(*Model)
	if !resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be true on Left key")
	}
	if cmd == nil {
		t.Fatal("expected fetch command on Left key")
	}
	msg := cmd().(leafMsg)
	if msg.leaf.Index != 4 {
		t.Errorf("expected fetch command for index 4, got %d", msg.leaf.Index)
	}

	// 2. Right arrow increments leaf index and returns fetch command
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	resM = newM.(*Model)
	if !resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be true on Right key")
	}
	if cmd == nil {
		t.Fatal("expected fetch command on Right key")
	}
	msg = cmd().(leafMsg)
	if msg.leaf.Index != 6 {
		t.Errorf("expected fetch command for index 6, got %d", msg.leaf.Index)
	}

	// 3. 'q' key quits in leaf view
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected tea.Quit command on 'q' in leaf view")
	}

	// 4. 'ctrl+c' quits globally
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Errorf("expected tea.Quit command on ctrl+c")
	}

	// 5. 'w' increments witnessN and triggers fetchCheckpointCmd
	resM.witnessN = 1
	newM, cmd = resM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	resM = newM.(*Model)
	if resM.witnessN != 2 {
		t.Errorf("expected witnessN to be 2, got %d", resM.witnessN)
	}
	if !resM.loadingCheck {
		t.Errorf("expected loadingCheck to be true on 'w'")
	}
	if cmd == nil {
		t.Errorf("expected fetchCheckpointCmd on 'w'")
	}

	// 6. 'W' decrements witnessN when > 1
	newM, cmd = resM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	resM = newM.(*Model)
	if resM.witnessN != 1 {
		t.Errorf("expected witnessN to decrement to 1, got %d", resM.witnessN)
	}
	if cmd == nil {
		t.Errorf("expected fetchCheckpointCmd on 'W'")
	}

	// 7. 'W' does not decrement when witnessN == 1
	resM.witnessN = 1
	newM, _ = resM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	resM = newM.(*Model)
	if resM.witnessN != 1 {
		t.Errorf("expected witnessN to remain 1, got %d", resM.witnessN)
	}

	// 8. 'l' switches to logs view
	newM, _ = resM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	resM = newM.(*Model)
	if resM.activeView != "logs" {
		t.Errorf("expected activeView to switch to 'logs', got %s", resM.activeView)
	}

	// 9. 'g' switches to jump view
	resM.activeView = "leaf"
	newM, _ = resM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	resM = newM.(*Model)
	if resM.activeView != "jump" {
		t.Errorf("expected activeView to switch to 'jump', got %s", resM.activeView)
	}
}

func TestUpdateJumpMode(t *testing.T) {
	client := &mockLogClient{leafBytes: []byte("leaf-data")}
	clients := map[string]logClient{"origin": client}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")
	m.checkpoint = &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 10}}

	// 1. Press esc in jump view cancels and returns to leaf view
	m.activeView = "jump"
	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	resM := newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after esc, got %s", resM.activeView)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd after esc in jump view")
	}

	// 2. Enter valid index jumps and fetches leaf
	m.activeView = "jump"
	m.textInput.SetValue("7")
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resM = newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after valid jump, got %s", resM.activeView)
	}
	if !resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be true after valid jump")
	}
	if cmd == nil {
		t.Fatal("expected fetch command after valid jump")
	}
	msg := cmd().(leafMsg)
	if msg.leaf.Index != 7 {
		t.Errorf("expected leaf fetch for index 7, got %d", msg.leaf.Index)
	}

	// 3. Enter out of bounds index resets to leaf view without fetching
	m.activeView = "jump"
	m.textInput.SetValue("15")
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resM = newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after out of bounds jump, got %s", resM.activeView)
	}
	if cmd != nil {
		t.Errorf("expected no command for out of bounds jump")
	}

	// 4. Non-numeric input resets to leaf view without fetching
	m.activeView = "jump"
	m.textInput.SetValue("notanumber")
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resM = newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after invalid input, got %s", resM.activeView)
	}
	if cmd != nil {
		t.Errorf("expected no command for invalid jump input")
	}
}

func TestUpdateLogsView(t *testing.T) {
	client1 := &mockLogClient{origin: "origin1"}
	client2 := &mockLogClient{origin: "origin2"}
	clients := map[string]logClient{
		"origin1": client1,
		"origin2": client2,
	}
	m := NewModel([]string{"origin1", "origin2"}, clients, &mockDistributor{}, nil, "origin1")
	m.activeView = "logs"

	// 1. 'esc' returns to leaf view
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	resM := newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after esc in logs view, got %s", resM.activeView)
	}

	// 2. 'q' returns to leaf view
	m.activeView = "logs"
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	resM = newM.(*Model)
	if resM.activeView != "leaf" {
		t.Errorf("expected activeView 'leaf' after 'q' in logs view, got %s", resM.activeView)
	}
}

func TestUpdateMessageHandling(t *testing.T) {
	client := &mockLogClient{origin: "origin", leafBytes: []byte("initial-leaf")}
	clients := map[string]logClient{"origin": client}
	m := NewModel([]string{"origin"}, clients, &mockDistributor{}, nil, "origin")
	m.viewport.Width = 80
	m.viewport.Height = 20

	// 1. checkpointMsg with error sets activeErr and clears loadingCheck
	m.loadingCheck = true
	newM, _ := m.Update(checkpointMsg{err: errors.New("failed to fetch checkpoint")})
	resM := newM.(*Model)
	if resM.loadingCheck {
		t.Errorf("expected loadingCheck to be false")
	}
	if resM.activeErr == nil || !strings.Contains(resM.activeErr.Error(), "failed to fetch checkpoint") {
		t.Errorf("expected activeErr to be set, got %v", resM.activeErr)
	}

	// 2. checkpointMsg with success and nil leaf fetches the latest leaf (size - 1)
	cp := &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 8}}
	newM, cmd := m.Update(checkpointMsg{checkpoint: cp})
	resM = newM.(*Model)
	if resM.checkpoint != cp {
		t.Errorf("expected checkpoint to be set")
	}
	if !resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be true")
	}
	if cmd == nil {
		t.Fatal("expected fetch command for latest leaf")
	}

	// 3. leafMsg with error sets error message in viewport
	m.loadingLeaf = true
	newM, _ = m.Update(leafMsg{err: errors.New("leaf download error")})
	resM = newM.(*Model)
	if resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be false")
	}
	if !strings.Contains(resM.viewport.View(), "leaf download error") {
		t.Errorf("expected viewport to display error, got %q", resM.viewport.View())
	}

	// 4. leafMsg with success updates leaf and formats into viewport
	m.loadingLeaf = true
	newM, _ = m.Update(leafMsg{leaf: model.Leaf{Index: 3, Contents: []byte("sample payload")}})
	resM = newM.(*Model)
	if resM.loadingLeaf {
		t.Errorf("expected loadingLeaf to be false")
	}
	if resM.leaf.Index != 3 {
		t.Errorf("expected leaf index 3, got %d", resM.leaf.Index)
	}
	if !strings.Contains(resM.viewport.View(), "formatted: sample payload") {
		t.Errorf("expected viewport to contain formatted payload, got %q", resM.viewport.View())
	}

	// 5. WindowSizeMsg updates width and height
	newM, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	resM = newM.(*Model)
	if resM.width != 120 || resM.height != 40 {
		t.Errorf("expected dimensions 120x40, got %dx%d", resM.width, resM.height)
	}
	if resM.viewport.Width != 120-6 {
		t.Errorf("expected viewport width %d, got %d", 120-6, resM.viewport.Width)
	}
}
