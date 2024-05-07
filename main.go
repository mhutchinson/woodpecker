package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	"github.com/transparency-dev/serverless-log/client"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	origin  = flag.String("origin", "transparency.dev/armored-witness/firmware_transparency/prod/1", "The origin of the log")
	vstring = flag.String("vkey", "transparency.dev-aw-ftlog-prod-1+3e6d87ee+Aa3qdhefd2cc/98jV3blslJT2L+iFR8WKHeGcgFmyjnt", "The verifier string for the log")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	logRoot, err := url.Parse("https://api.transparency.dev/armored-witness-firmware/prod/log/1/")
	if err != nil {
		klog.Exit(err)
	}
	dirtyChannel := make(chan bool, 1)
	model := &model.ViewModel{
		Dirty: dirtyChannel,
	}
	fetcher := newFetcher(logRoot)
	controller := Controller{
		Model:     model,
		LogClient: newLogClient(fetcher),
		Fetcher:   fetcher,
	}
	controller.RefreshCheckpoint()
	if model.GetCheckpoint() != nil && model.GetCheckpoint().Size > 0 {
		controller.GetLeaf(model.GetCheckpoint().Size - 1)
	}
	view := NewView(controller, model)
	if err := view.Run(context.Background()); err != nil {
		panic(err)
	}
}

type Controller struct {
	Model     *model.ViewModel
	LogClient *logClient
	Fetcher   client.Fetcher
}

func (c Controller) RefreshCheckpoint() {
	c.Model.SetCheckpoint(c.LogClient.getCheckpoint())
}

func (c Controller) GetLeaf(index uint64) {
	if index >= c.Model.GetCheckpoint().Size {
		c.Model.SetLeaf(c.Model.GetLeaf(), fmt.Errorf("Cannot fetch leaf bigger than checkpoint size %d", c.Model.GetCheckpoint().Size))
		return
	}
	leaf, err := client.GetLeaf(context.Background(), c.Fetcher, index)
	c.Model.SetLeaf(model.Leaf{
		Contents: leaf,
		Index:    index,
	}, err)
}

func (c Controller) PrevLeaf() {
	c.GetLeaf(c.Model.GetLeaf().Index - 1)
}

func (c Controller) NextLeaf() {
	c.GetLeaf(c.Model.GetLeaf().Index + 1)
}

func newLogClient(fetcher client.Fetcher) *logClient {
	verifier, err := note.NewVerifier(*vstring)
	if err != nil {
		panic(err)
	}
	return &logClient{
		origin:   *origin,
		verifier: verifier,
		fetcher:  fetcher,
	}
}

type logClient struct {
	origin   string
	verifier note.Verifier
	fetcher  client.Fetcher
}

func (lc *logClient) getCheckpoint() (*log.Checkpoint, error) {
	cp, _, _, err := client.FetchCheckpoint(context.Background(), lc.fetcher, lc.verifier, lc.origin)
	return cp, err
}

// newFetcher creates a Fetcher for the log at the given root location.
func newFetcher(root *url.URL) client.Fetcher {
	get := getByScheme[root.Scheme]
	if get == nil {
		panic(fmt.Errorf("unsupported URL scheme %s", root.Scheme))
	}

	return func(ctx context.Context, p string) ([]byte, error) {
		u, err := root.Parse(p)
		if err != nil {
			return nil, err
		}
		return get(ctx, u)
	}
}

var getByScheme = map[string]func(context.Context, *url.URL) ([]byte, error){
	"http":  readHTTP,
	"https": readHTTP,
	"file": func(_ context.Context, u *url.URL) ([]byte, error) {
		return os.ReadFile(u.Path)
	},
}

func readHTTP(ctx context.Context, u *url.URL) ([]byte, error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case 404:
		klog.Infof("Not found: %q", u.String())
		return nil, os.ErrNotExist
	case 200:
		break
	default:
		return nil, fmt.Errorf("unexpected http status %q", resp.Status)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Errorf("resp.Body.Close(): %v", err)
		}
	}()
	return io.ReadAll(resp.Body)
}
