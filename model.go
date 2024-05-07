package main

import "github.com/transparency-dev/formats/log"

type ViewModel struct {
	Checkpoint *log.Checkpoint
	Leaf       Leaf
	Error      error
}

type Leaf struct {
	Contents []byte
	Index    uint64
}
