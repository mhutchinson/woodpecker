package model

import "github.com/transparency-dev/formats/log"

func NewViewModel(logOrigins []string) *ViewModel {
	return &ViewModel{
		Dirty:      make(chan bool, 1),
		logOrigins: logOrigins,
	}
}

type ViewModel struct {
	Dirty      chan bool
	logOrigins []string
	checkpoint *log.Checkpoint
	witnessed  *log.Checkpoint
	leaf       Leaf
	error      error
}

func (m *ViewModel) SetCheckpoint(cp *log.Checkpoint, witnessedCP *log.Checkpoint, err error) {
	m.checkpoint = cp
	m.witnessed = witnessedCP
	m.error = err
	m.setDirty()
}

func (m *ViewModel) SetLeaf(leaf Leaf, err error) {
	m.leaf = leaf
	m.error = err
	m.setDirty()
}

func (m *ViewModel) setDirty() {
	select {
	case m.Dirty <- true:
	default:
	}
}

func (m *ViewModel) GetLogOrigins() []string {
	return m.logOrigins
}

func (m *ViewModel) GetCheckpoint() *log.Checkpoint {
	return m.checkpoint
}

func (m *ViewModel) GetLeaf() Leaf {
	return m.leaf
}

func (m *ViewModel) GetError() error {
	return m.error
}

type Leaf struct {
	Contents []byte
	Index    uint64
}
