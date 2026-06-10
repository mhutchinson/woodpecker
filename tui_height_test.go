package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
)

func TestTUIHeightAndContent(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{"Small height 15", 15},
		{"Medium height 20", 20},
		{"Standard height 24", 24},
		{"Large height 40", 40},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize a mock model using existing mockLogClient
			clients := map[string]logClient{
				"test-log": &mockLogClient{},
			}
			m := NewModel(
				[]string{"test-log"},
				clients,
				&mockDistributor{},
				nil,
				"test-log",
			)

			// Set window size
			m.Update(tea.WindowSizeMsg{Width: 80, Height: tc.height})

			// Set some checkpoint and leaf so we are in the default leaf view
			m.loadingCheck = false
			m.loadingLeaf = false

			// Create a checkpoint note with 10 lines of text to trigger truncation/max-height rendering
			longRaw := "checkpoint data line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\nline 12"
			m.checkpoint = &model.Checkpoint{
				Checkpoint: &log.Checkpoint{Size: 100},
				Raw:        []byte(longRaw),
			}
			m.leaf = model.Leaf{
				Index:    42,
				Contents: []byte("leaf data line 1\nleaf data line 2\nleaf data line 3"),
			}
			m.viewport.SetContent(string(m.leaf.Contents))

			// Get the TUI View
			view := m.View()
			lines := strings.Split(view, "\n")

			t.Logf("Terminal Height: %d", m.height)
			t.Logf("Rendered TUI lines: %d", len(lines))

			if len(lines) > m.height {
				for i, l := range lines {
					t.Logf("%02d: %q", i+1, l)
				}
				t.Errorf("FAIL: Rendered lines (%d) exceeded terminal height (%d)!", len(lines), m.height)
			}
		})
	}
}
