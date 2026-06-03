package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mhutchinson/woodpecker/model"
	distclient "github.com/transparency-dev/distributor/client"
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

type mockLogClient struct{}

func (m *mockLogClient) GetOrigin() string                         { return "origin" }
func (m *mockLogClient) GetVerifier() note.Verifier                { return nil }
func (m *mockLogClient) GetCheckpoint() (*model.Checkpoint, error) { return nil, nil }
func (m *mockLogClient) GetLeaf(size, index uint64) ([]byte, error) {
	return []byte("leaf"), nil
}
func (m *mockLogClient) FormatLeaf(leaf []byte) string {
	return string(leaf)
}
func (m *mockLogClient) GetLogType() string { return "mock" }
func (m *mockLogClient) GetURL() string     { return "http://mock" }

func TestPrevLeafUnderflow(t *testing.T) {
	clients := map[string]logClient{
		"origin": &mockLogClient{},
	}
	m := NewModel([]string{"origin"}, clients, distclient.RestDistributor{}, nil, "origin")

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
	m := NewModel([]string{"origin"}, clients, distclient.RestDistributor{}, nil, "origin")

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
