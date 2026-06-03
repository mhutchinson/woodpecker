package model

import (
	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
)

type Checkpoint struct {
	*log.Checkpoint
	Note *note.Note
	Raw  []byte
}

type Leaf struct {
	Contents []byte
	Index    uint64
}
