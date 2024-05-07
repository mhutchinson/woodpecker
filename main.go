package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"

	"github.com/transparency-dev/formats/log"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	origin  = flag.String("origin", "go.sum database tree", "The origin of the log")
	vstring = flag.String("vkey", "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8", "The verifier string for the log")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	model := &ViewModel{}
	controller := Controller{
		Model:     model,
		LogClient: newLogClient(),
	}
	controller.RefreshCheckpoint()
	if model.Checkpoint != nil && model.Checkpoint.Size > 0 {
		controller.GetLeaf(model.Checkpoint.Size - 1)
	}
	view := NewView(controller, model)
	if err := view.Run(context.Background()); err != nil {
		panic(err)
	}
}

type Controller struct {
	Model     *ViewModel
	LogClient *logClient
}

func (c Controller) RefreshCheckpoint() {
	c.Model.Checkpoint, c.Model.Error = c.LogClient.getCheckpoint()
}

func (c Controller) GetLeaf(index uint64) {
	if index >= c.Model.Checkpoint.Size {
		c.Model.Error = fmt.Errorf("Cannot fetch leaf bigger than checkpoint size %d", c.Model.Checkpoint.Size)
		return
	}
	c.Model.Leaf = Leaf{
		Contents: []byte(fmt.Sprintf("hello %d", index)),
		Index:    index,
	}
	c.Model.Error = nil
}

func newLogClient() *logClient {
	verifier, err := note.NewVerifier(*vstring)
	if err != nil {
		panic(err)
	}
	return &logClient{
		origin:   *origin,
		verifier: verifier,
	}
}

type logClient struct {
	origin   string
	verifier note.Verifier
}

func (lc *logClient) getCheckpoint() (*log.Checkpoint, error) {
	resp, err := http.DefaultClient.Get("http://sum.golang.org/latest")
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	cp, _, _, err := log.ParseCheckpoint(raw, lc.origin, lc.verifier)
	return cp, err
}
