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
	distclient "github.com/transparency-dev/distributor/client"
	"github.com/transparency-dev/formats/log"
	tnote "github.com/transparency-dev/formats/note"
	"github.com/transparency-dev/serverless-log/client"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

const distURL = "https://api.transparency.dev"

var (
	clients = []logClient{
		newServerlessLogClient("https://api.transparency.dev/armored-witness-firmware/prod/log/1/",
			"transparency.dev/armored-witness/firmware_transparency/prod/1",
			"transparency.dev-aw-ftlog-prod-1+3e6d87ee+Aa3qdhefd2cc/98jV3blslJT2L+iFR8WKHeGcgFmyjnt"),
		newServerlessLogClient("https://api.transparency.dev/armored-witness-firmware/ci/log/4/",
			"transparency.dev/armored-witness/firmware_transparency/ci/4",
			"transparency.dev-aw-ftlog-ci-4+30fe79e3+AUDoas+smwQDTlYbTzbEcAW+N6WyvB/4CysMWjpnRgat"),
		newServerlessLogClient("https://raw.githubusercontent.com/f-secure-foundry/armory-drive-log/master/log/",
			"Armory Drive Prod 2",
			"armory-drive-log+16541b8f+AYDPmG5pQp4Bgu0a1mr5uDZ196+t8lIVIfWQSPWmP+Jv"),
		newServerlessLogClient("https://fwupd.org/ftlog/lvfs/",
			"lvfs",
			"lvfs+7908d142+ASnlGgOh+634tcE/2Lp3wV7k/cLoU6ncawmb/BLC1oMU"),
	}
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	logClients := make(map[string]logClient, len(clients))
	logOrigins := make([]string, 0, len(clients))
	for _, c := range clients {
		logClients[c.GetOrigin()] = c
		logOrigins = append(logOrigins, c.GetOrigin())
	}
	model := model.NewViewModel(logOrigins)
	controller := NewController(model, logClients, *distclient.NewRestDistributor(distURL, http.DefaultClient))
	controller.SelectLog(clients[0].GetOrigin())
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
	view := NewView(controller, model)
	if err := view.Run(context.Background()); err != nil {
		panic(err)
	}
}

func NewController(model *model.ViewModel, logClients map[string]logClient, distributor distclient.RestDistributor) *Controller {
	witKeys, err := distributor.GetWitnesses()
	if err != nil {
		panic(fmt.Sprintf("Witnesses not available: %v", err))
	}
	witVerifiers := make([]note.Verifier, 0, len(witKeys))
	for _, k := range witKeys {
		v, err := tnote.NewVerifierForCosignatureV1(k)
		if err != nil {
			panic(fmt.Sprintf("Invalid witness key: %v", err))
		}
		witVerifiers = append(witVerifiers, v)
	}
	return &Controller{
		Model:        model,
		LogClients:   logClients,
		Distributor:  distributor,
		witVerifiers: witVerifiers,
		witnessSigs:  2,
	}
}

type Controller struct {
	Model        *model.ViewModel
	LogClients   map[string]logClient
	Distributor  distclient.RestDistributor
	witVerifiers []note.Verifier

	current     logClient
	witnessSigs uint
}

func (c *Controller) SelectLog(o string) {
	if n, ok := c.LogClients[o]; ok {
		c.current = n
		c.InitFromLog()
	}
}

func (c *Controller) InitFromLog() {
	c.RefreshCheckpoint()
	if c.Model.GetCheckpoint() != nil && c.Model.GetCheckpoint().Size > 0 {
		c.GetLeaf(c.Model.GetCheckpoint().Size - 1)
	}
}

func (c *Controller) RefreshCheckpoint() {
	witnessed := make(chan *model.Checkpoint)
	// Fetch the witnessed checkpoint in parallel
	go func() {
		logID := distclient.LogID(log.ID(c.current.GetOrigin()))
		bs, err := c.Distributor.GetCheckpointN(logID, c.witnessSigs)
		if err != nil {
			witnessed <- nil
			return
		}
		cp, _, n, _ := log.ParseCheckpoint(bs, c.current.GetOrigin(), c.current.GetVerifier(), c.witVerifiers...)
		witnessed <- &model.Checkpoint{
			Checkpoint: cp,
			Note:       n,
			Raw:        bs,
		}
	}()
	cp, err := c.current.GetCheckpoint()
	wCP := <-witnessed
	c.Model.SetCheckpoint(cp, wCP, err)
}

func (c *Controller) GetLeaf(index uint64) {
	size := c.Model.GetCheckpoint().Size
	if index >= size {
		c.Model.SetLeaf(c.Model.GetLeaf(), fmt.Errorf("Cannot fetch leaf bigger than checkpoint size %d", size))
		return
	}
	leaf, err := c.current.GetLeaf(index)
	c.Model.SetLeaf(model.Leaf{
		Contents: leaf,
		Index:    index,
	}, err)
}

func (c *Controller) PrevLeaf() {
	c.GetLeaf(c.Model.GetLeaf().Index - 1)
}

func (c *Controller) NextLeaf() {
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
	GetOrigin() string
	GetVerifier() note.Verifier
	GetCheckpoint() (*model.Checkpoint, error)
	GetLeaf(uint64) ([]byte, error)
}

type serverlessLogClient struct {
	origin   string
	verifier note.Verifier
	fetcher  client.Fetcher
}

func (c *serverlessLogClient) GetOrigin() string {
	return c.origin
}

func (c *serverlessLogClient) GetVerifier() note.Verifier {
	return c.verifier
}

func (c *serverlessLogClient) GetCheckpoint() (*model.Checkpoint, error) {
	cp, raw, n, err := client.FetchCheckpoint(context.Background(), c.fetcher, c.verifier, c.origin)
	return &model.Checkpoint{
		Checkpoint: cp,
		Raw:        raw,
		Note:       n,
	}, err
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
