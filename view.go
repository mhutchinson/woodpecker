package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rivo/tview"
)

type Callbacks interface {
	RefreshCheckpoint()
	GetLeaf(uint64)
}

type View struct {
	Model     *ViewModel
	Callbacks Callbacks

	app      *tview.Application
	cpArea   *tview.TextView
	leafArea *tview.TextView
	errArea  *tview.TextView
}

func NewView(cb Callbacks, m *ViewModel) View {
	grid := tview.NewGrid()
	grid.SetRows(8, 0, 3).SetColumns(0).SetBorders(true)
	cpArea := tview.NewTextView()
	leafArea := tview.NewTextView()
	leafArea.SetBorder(true).SetTitle("No leaf loaded")
	errArea := tview.NewTextView()
	app := tview.NewApplication()
	app.SetRoot(grid, true)

	grid.AddItem(cpArea, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(leafArea, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(errArea, 2, 0, 1, 1, 0, 0, false)

	v := View{
		Model:     m,
		Callbacks: cb,
		app:       app,
		cpArea:    cpArea,
		leafArea:  leafArea,
		errArea:   errArea,
	}
	return v
}

func (v View) Run(ctx context.Context) error {
	go func() {
		t := time.NewTicker(5 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			v.refreshCheckpoint()
			v.app.Draw()
		}
	}()
	v.leafArea.SetTitle(fmt.Sprintf("Leaf %d", v.Model.Leaf.Index))
	v.leafArea.SetText(string(v.Model.Leaf.Contents))
	return v.app.Run()
}

func (v View) refreshCheckpoint() {
	v.Callbacks.RefreshCheckpoint()
	cp := v.Model.Checkpoint
	if cp != nil {
		text := string(cp.Marshal())
		v.cpArea.SetText(text)
	}
	if v.Model.Error != nil {
		v.errArea.SetText(fmt.Sprint(v.Model.Error))
	} else {
		v.errArea.SetText("")
	}
}
