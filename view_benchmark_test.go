package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

// Mock Note structure if needed, or use the real one.
// definition of note.Note: https://pkg.go.dev/golang.org/x/mod/sumdb/note#Note
// type Note struct {
//    Text string
//    Sigs []Signature
// }
// type Signature struct {
//    Name string
//    Base64 string
//    Hash uint32
// }

func getMockCheckpoint(numSigs int) *model.Checkpoint {
	sigs := make([]note.Signature, numSigs)
	for i := 0; i < numSigs; i++ {
		sigs[i] = note.Signature{
			Name: fmt.Sprintf("Witness %d", i),
		}
	}

	return &model.Checkpoint{
		Checkpoint: &log.Checkpoint{
			Size: 12345,
			Hash: []byte("mockhash"),
		},
		Note: &note.Note{
			Sigs: sigs,
		},
	}
}

func BenchmarkStringConcatenation(b *testing.B) {
	// Create a checkpoint with a reasonable number of signatures
	wit := getMockCheckpoint(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text := fmt.Sprintf("Size: %d | Hash: %x", wit.Size, wit.Hash)
		// Simulate the logic in view.go
		// wits := wit.Note.Sigs[1:] // The original code does this
		// But for benchmark we should ensure we have enough sigs to slice
		if len(wit.Note.Sigs) > 1 {
			wits := wit.Note.Sigs[1:]
			for _, w := range wits {
				text = fmt.Sprintf("%s\n%s", text, w.Name)
			}
		}
		_ = text
	}
}

func BenchmarkStringBuilder(b *testing.B) {
	wit := getMockCheckpoint(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Size: %d | Hash: %x", wit.Size, wit.Hash)

		if len(wit.Note.Sigs) > 1 {
			wits := wit.Note.Sigs[1:]
			for _, w := range wits {
				// Use WriteString or Fprintf
				sb.WriteString("\n")
				sb.WriteString(w.Name)
			}
		}
		_ = sb.String()
	}
}
