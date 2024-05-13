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
	SelectLog(o string)
}

type View struct {
	Model     *model.ViewModel
	Callbacks Callbacks

	app      *tview.Application
	cpArea   *tview.TextView
	witArea  *tview.TextView
	mainArea *tview.Pages
	leafPage *tview.TextView
	logsPage *tview.List
	errArea  *tview.TextView
}

func NewView(cb Callbacks, m *model.ViewModel) View {
	app := tview.NewApplication()
	grid := tview.NewGrid()
	grid.SetRows(15, 0, 3).SetColumns(0)
	cpArea := tview.NewTextView()
	cpArea.SetBorder(true).SetTitle("Log Checkpoint")
	witnessedArea := tview.NewTextView()
	witnessedArea.SetBorder(true).SetTitle("Witnessed Checkpoint")

	mainArea := tview.NewPages()
	leafPage := tview.NewTextView()
	leafPage.SetBorder(true).SetTitle("No leaf loaded")
	logsPage := tview.NewList()
	logsPage.SetBorder(true).SetTitle("Choose a log to investigate")
	for i, o := range m.GetLogOrigins() {
		logsPage.AddItem(o, "", rune('a'+i), func() {
			cb.SelectLog(o)
		})
	}
	exitLogSelector := func() {
		mainArea.SwitchToPage("leaf")
		app.SetFocus(mainArea)
	}
	logsPage.AddItem("eXit", "eXplore the selected log", rune('x'), exitLogSelector)
	logsPage.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			exitLogSelector()
			return nil
		case tcell.KeyEscape:
			exitLogSelector()
			return nil
		}
		return event
	})
	mainArea.AddPage("logs", logsPage, true, true)
	mainArea.AddPage("leaf", leafPage, true, true)

	errArea := tview.NewTextView()
	app.SetRoot(grid, true)

	cpFlex := tview.NewFlex()
	cpFlex.AddItem(cpArea, 0, 1, false)
	cpFlex.AddItem(witnessedArea, 0, 1, false)
	grid.AddItem(cpFlex, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(mainArea, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(errArea, 2, 0, 1, 1, 0, 0, false)

	v := View{
		Model:     m,
		Callbacks: cb,
		app:       app,
		cpArea:    cpArea,
		witArea:   witnessedArea,
		mainArea:  mainArea,
		leafPage:  leafPage,
		logsPage:  logsPage,
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
		switch event.Rune() {
		case 'l':
			v.mainArea.SwitchToPage("logs")
			v.app.SetFocus(v.logsPage)
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
		text := string(cp.Raw)
		v.cpArea.SetText(text)
	}
	wit := v.Model.GetWitnessed()
	if wit != nil {
		v.witArea.SetText(fmt.Sprintf("Size: %d\nWitnesses: %d", wit.Size, len(wit.Note.Sigs)-1))
	}

	v.leafPage.SetTitle(fmt.Sprintf("Leaf %d", v.Model.GetLeaf().Index))
	v.leafPage.SetText(string(v.Model.GetLeaf().Contents))

	if v.Model.GetError() != nil {
		v.errArea.SetText(fmt.Sprintf("Error: %v", v.Model.GetError()))
	} else {
		v.errArea.SetText("")
	}
}
