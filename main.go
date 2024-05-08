package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	"github.com/transparency-dev/serverless-log/client"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	clients = []logClient{
		newServerlessLogClient("https://api.transparency.dev/armored-witness-firmware/prod/log/1/",
			"transparency.dev/armored-witness/firmware_transparency/prod/1",
			"transparency.dev-aw-ftlog-prod-1+3e6d87ee+Aa3qdhefd2cc/98jV3blslJT2L+iFR8WKHeGcgFmyjnt"),
	}
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	dirtyChannel := make(chan bool, 1)
	model := &model.ViewModel{
		Dirty: dirtyChannel,
	}
	controller := Controller{
		Model:     model,
		LogClient: clients[0],
	}
	go func() {
		t := time.NewTicker(5 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				controller.RefreshCheckpoint()
			}
		}
	}()
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
	LogClient logClient
}

func (c Controller) RefreshCheckpoint() {
	cp, err := c.LogClient.GetCheckpoint()
	c.Model.SetCheckpoint(cp, err)
}

func (c Controller) GetLeaf(index uint64) {
	size := c.Model.GetCheckpoint().Size
	if index >= size {
		c.Model.SetLeaf(c.Model.GetLeaf(), fmt.Errorf("Cannot fetch leaf bigger than checkpoint size %d", size))
		return
	}
	leaf, err := c.LogClient.GetLeaf(index)
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

func newServerlessLogClient(lr string, origin string, vkey string) logClient {
	logRoot, err := url.Parse(lr)
	if err != nil {
		klog.Exit(err)
	}
	fetcher := newFetcher(logRoot)
	verifier, err := note.NewVerifier(vkey)
	if err != nil {
		klog.Exit(err)
	}
	return &serverlessLogClient{
		origin:   origin,
		verifier: verifier,
		fetcher:  fetcher,
	}
}

type logClient interface {
	GetCheckpoint() (*log.Checkpoint, error)
	GetLeaf(uint64) ([]byte, error)
}

type serverlessLogClient struct {
	origin   string
	verifier note.Verifier
	fetcher  client.Fetcher
}

func (c *serverlessLogClient) GetCheckpoint() (*log.Checkpoint, error) {
	cp, _, _, err := client.FetchCheckpoint(context.Background(), c.fetcher, c.verifier, c.origin)
	return cp, err
}

func (c *serverlessLogClient) GetLeaf(index uint64) ([]byte, error) {
	leaf, err := client.GetLeaf(context.Background(), c.fetcher, index)
	return leaf, err
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
