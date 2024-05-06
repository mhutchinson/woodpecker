package main

import (
	"flag"
	"io"
	"net/http"

	"github.com/rivo/tview"
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

	logClient := newLogClient()
	textArea := tview.NewTextView()
	textArea.SetBorder(true).SetTitle("Hello, world!")
	go func() {
		cp, err := logClient.getCheckpoint()
		if err != nil {
			klog.Warning(err)
		}
		text := string(cp.Marshal())
		textArea.SetText(text)
	}()
	if err := tview.NewApplication().SetRoot(textArea, true).Run(); err != nil {
		panic(err)
	}
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
