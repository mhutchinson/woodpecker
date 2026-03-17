package main

import (
	"testing"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

type mockLogClient struct{}

func (m *mockLogClient) GetOrigin() string                             { return "origin" }
func (m *mockLogClient) GetVerifier() note.Verifier                   { return nil }
func (m *mockLogClient) GetCheckpoint() (*model.Checkpoint, error)    { return nil, nil }
func (m *mockLogClient) GetLeaf(size, index uint64) ([]byte, error) { return []byte("leaf"), nil }

func TestPrevLeafUnderflow(t *testing.T) {
	m := model.NewViewModel([]string{"origin"})
	c := &Controller{
		Model:   m,
		current: &mockLogClient{},
	}

	// Set up model state
	m.SetCheckpoint(&model.Checkpoint{
		Checkpoint: &log.Checkpoint{Size: 10},
	}, nil, nil)
	m.SetLeaf(model.Leaf{Index: 0}, nil)

	// This should no longer underflow and should NOT call GetLeaf
	c.PrevLeaf()

	if err := m.GetError(); err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	if m.GetLeaf().Index != 0 {
		t.Errorf("Expected leaf index to remain 0, but got %d", m.GetLeaf().Index)
	}
}

func TestNextLeafOverflow(t *testing.T) {
	m := model.NewViewModel([]string{"origin"})
	c := &Controller{
		Model:   m,
		current: &mockLogClient{},
	}

	// Set up model state: last leaf is at index 9 for size 10
	m.SetCheckpoint(&model.Checkpoint{
		Checkpoint: &log.Checkpoint{Size: 10},
	}, nil, nil)
	m.SetLeaf(model.Leaf{Index: 9}, nil)

	// This should no longer go out of bounds and should NOT call GetLeaf
	c.NextLeaf()

	if err := m.GetError(); err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	if m.GetLeaf().Index != 9 {
		t.Errorf("Expected leaf index to remain 9, but got %d", m.GetLeaf().Index)
	}
}
