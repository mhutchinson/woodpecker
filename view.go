package main

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/mhutchinson/woodpecker/model"
	"github.com/rivo/tview"
)

type Callbacks interface {
	RefreshCheckpoint()
	GetLeaf(uint64)
	PrevLeaf()
	NextLeaf()
}

type View struct {
	Model     *model.ViewModel
	Callbacks Callbacks

	app      *tview.Application
	cpArea   *tview.TextView
	leafArea *tview.TextView
	errArea  *tview.TextView
}

func NewView(cb Callbacks, m *model.ViewModel) View {
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
	v.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyLeft:
			v.Callbacks.PrevLeaf()
			return nil
		case tcell.KeyRight:
			v.Callbacks.NextLeaf()
			return nil
		}
		return event
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-v.Model.Dirty:
				v.refreshFromModel()
				v.app.Draw()
			}
		}
	}()
	return v.app.Run()
}

func (v View) refreshFromModel() {
	cp := v.Model.GetCheckpoint()
	if cp != nil {
		text := string(cp.Marshal())
		v.cpArea.SetText(text)
	}

	v.leafArea.SetTitle(fmt.Sprintf("Leaf %d", v.Model.GetLeaf().Index))
	v.leafArea.SetText(string(v.Model.GetLeaf().Contents))

	if v.Model.GetError() != nil {
		v.errArea.SetText(fmt.Sprintf("Error: %v", v.Model.GetError()))
	} else {
		v.errArea.SetText("")
	}
}
