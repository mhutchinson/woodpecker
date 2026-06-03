package main

import (
	"context"
	"strings"
	"testing"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
)

func TestSumDBLogClientGetLeaf_Negative(t *testing.T) {
	client := &sumDBLogClient{
		url:    "http://example.com",
		origin: "origin",
	}

	// 1. Nil checkpoint
	if _, err := client.GetLeaf(nil, 0); err == nil {
		t.Error("expected error for nil checkpoint, got nil")
	}

	checkpoint := &model.Checkpoint{
		Checkpoint: &log.Checkpoint{
			Size: 10,
		},
	}

	// 2. Out of bounds index
	if _, err := client.GetLeaf(checkpoint, 10); err == nil {
		t.Error("expected error for index >= size, got nil")
	}
}

func TestSumDBLogClientGetLeaf_TruncatedTile(t *testing.T) {
	mockFetcher := func(ctx context.Context, path string) ([]byte, error) {
		// Return only 2 leaves (separated by \n\n)
		return []byte("leaf0\n\nleaf1"), nil
	}

	client := &sumDBLogClient{
		url:     "http://example.com",
		origin:  "origin",
		fetcher: mockFetcher,
	}

	// Size 10, index 5. leafOffset will be 5.
	// We only return 2 leaves, so it should fail.
	checkpoint := &model.Checkpoint{
		Checkpoint: &log.Checkpoint{
			Size: 10,
			Hash: make([]byte, 32), // dummy hash
		},
	}

	_, err := client.GetLeaf(checkpoint, 5)
	if err == nil {
		t.Fatal("expected error for truncated tile data, got nil")
	}
	if !strings.Contains(err.Error(), "tile data truncated") {
		t.Errorf("expected 'tile data truncated' error, got: %v", err)
	}
}
