package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/sunlight"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mhutchinson/woodpecker/model"
	distclient "github.com/transparency-dev/distributor/client"
	"github.com/transparency-dev/formats/log"
	tnote "github.com/transparency-dev/formats/note"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	serverless_client "github.com/transparency-dev/serverless-log/client"
	tiles_client "github.com/transparency-dev/trillian-tessera/client"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
	"golang.org/x/sync/singleflight"
	"k8s.io/klog/v2"
)

const distURL = "https://api.transparency.dev"

var clients []logClient

var (
	httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

var (
	origin          = flag.String("origin", "", "The origin of a built-in log to open by default")
	customLogUrl    = flag.String("custom_log_url", "", "The base URL of a custom log to register")
	customLogOrigin = flag.String("custom_log_origin", "", "The origin of a custom log to register")
	customLogVKey   = flag.String("custom_log_vkey", "", "The verifier key of a custom log to register")
	customLogType   = flag.String("custom_log_type", "", "The type of the custom log specified by the other custom_* flags. Must be empty, or one of {tiles, serverless, static-ct}.")
)

func initLogging() (*os.File, error) {
	// Check if user explicitly requested logging to stderr
	logToStderr := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "logtostderr" && f.Value.String() == "true" {
			logToStderr = true
		}
		if f.Name == "alsologtostderr" && f.Value.String() == "true" {
			logToStderr = true
		}
	})

	if logToStderr {
		return nil, nil
	}

	// Otherwise, redirect klog to a file
	logPath := filepath.Join(os.TempDir(), "woodpecker.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		// Fallback to discard if we can't write to file
		klog.SetOutput(io.Discard)
		return nil, err
	}

	klog.SetOutput(logFile)
	// Try to set logtostderr to false to prevent it from bypassing SetOutput
	_ = flag.Set("logtostderr", "false")
	_ = flag.Set("alsologtostderr", "false")
	// Only send FATAL logs to stderr (which exits anyway)
	_ = flag.Set("stderrthreshold", "3")

	return logFile, nil
}

func main() {
	flag.Parse()
	if lf, err := initLogging(); err == nil && lf != nil {
		defer func() {
			if err := lf.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", err)
			}
		}()
	}
	defer klog.Flush()

	// Initialize built-in clients
	builtInClients := []struct {
		url, origin, vkey string
		logType           string // "serverless", "sumdb", "tiles"
	}{
		{
			url:     "https://sum.golang.org/",
			origin:  "go.sum database tree",
			vkey:    "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8",
			logType: "sumdb",
		},
		{
			url:     "https://log2025-1.rekor.sigstore.dev/api/v2/",
			origin:  "log2025-1.rekor.sigstore.dev",
			vkey:    "log2025-1.rekor.sigstore.dev+cf119915+AbfK5adZJxsI323FwGD2AJJ9F4i89cfDuLdGJBIYntuO",
			logType: "tiles",
		},
		{
			url:     "https://api.transparency.dev/armored-witness-firmware/prod/log/1/",
			origin:  "transparency.dev/armored-witness/firmware_transparency/prod/1",
			vkey:    "transparency.dev-aw-ftlog-prod-1+3e6d87ee+Aa3qdhefd2cc/98jV3blslJT2L+iFR8WKHeGcgFmyjnt",
			logType: "serverless",
		},
		{
			url:     "https://api.transparency.dev/armored-witness-firmware/ci/log/4/",
			origin:  "transparency.dev/armored-witness/firmware_transparency/ci/4",
			vkey:    "transparency.dev-aw-ftlog-ci-4+30fe79e3+AUDoas+smwQDTlYbTzbEcAW+N6WyvB/4CysMWjpnRgat",
			logType: "serverless",
		},
		{
			url:     "https://raw.githubusercontent.com/f-secure-foundry/armory-drive-log/master/log/",
			origin:  "Armory Drive Prod 2",
			vkey:    "armory-drive-log+16541b8f+AYDPmG5pQp4Bgu0a1mr5uDZ196+t8lIVIfWQSPWmP+Jv",
			logType: "serverless",
		},
		{
			url:     "https://storage.googleapis.com/coachandhorses2026h1.staging.certificate.transparency.goog/",
			origin:  "coachandhorses2026h1.staging.certificate.transparency.goog",
			vkey:    "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAECHOhXfvYgTcu+Fnl7M7niFj3FgqWlQpXUSWUDw2KAaJXvhGxdJTtmyciN5rWTiDtpeNENVmsUTHFS4XQgeRE0g==",
			logType: "static-ct",
		},
	}

	for _, c := range builtInClients {
		var client logClient
		var err error
		switch c.logType {
		case "serverless":
			client, err = newServerlessLogClient(c.url, c.origin, c.vkey)
		case "sumdb":
			client, err = newSumDBLogClient(c.url, c.origin, c.vkey)
		case "tiles":
			client, err = newTLogTilesLogClient(c.url, c.origin, c.vkey)
		case "static-ct":
			client, err = newStaticCTLogClient(c.url, c.origin, c.vkey)
		}
		if err != nil {
			panic(fmt.Sprintf("Failed to initialize built-in client for %s: %v", c.origin, err))
		}
		clients = append(clients, client)
	}

	switch *customLogType {
	case "":
		break
	case "tiles":
		c, err := newTLogTilesLogClient(*customLogUrl, *customLogOrigin, *customLogVKey)
		if err != nil {
			klog.Exitf("Failed to initialize custom tiles log: %v", err)
		}
		clients = append([]logClient{c}, clients...)
	case "serverless":
		c, err := newServerlessLogClient(*customLogUrl, *customLogOrigin, *customLogVKey)
		if err != nil {
			klog.Exitf("Failed to initialize custom serverless log: %v", err)
		}
		clients = append([]logClient{c}, clients...)
	case "static-ct":
		c, err := newStaticCTLogClient(*customLogUrl, *customLogOrigin, *customLogVKey)
		if err != nil {
			klog.Exitf("Failed to initialize custom static-ct log: %v", err)
		}
		clients = append([]logClient{c}, clients...)
	default:
		klog.Exitf("custom_log_type %s not recognised", *customLogType)
	}
	logClients := make(map[string]logClient, len(clients))
	logOrigins := make([]string, 0, len(clients))
	for _, c := range clients {
		logClients[c.GetOrigin()] = c
		logOrigins = append(logOrigins, c.GetOrigin())
	}

	dist := distclient.NewRestDistributor(distURL, httpClient)
	witKeys, err := dist.GetWitnesses()
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

	initialLog := clients[0].GetOrigin()
	if len(*origin) > 0 {
		for _, c := range clients {
			if *origin == c.GetOrigin() {
				initialLog = c.GetOrigin()
				break
			}
		}
	}

	pModel := NewModel(logOrigins, logClients, dist, witVerifiers, initialLog)
	p := tea.NewProgram(pModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}

type logClient interface {
	GetOrigin() string
	GetVerifier() note.Verifier
	GetCheckpoint() (*model.Checkpoint, error)
	GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error)
	FormatLeaf(leaf []byte) string
	GetLogType() string
	GetURL() string
}

func newTLogTilesLogClient(lr string, origin string, vkey string) (logClient, error) {
	if !strings.HasSuffix(lr, "/") {
		lr = lr + "/"
	}
	logRoot, err := url.Parse(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL %q: %w", lr, err)
	}
	fetcher := newFetcher(logRoot)
	verifier, err := note.NewVerifier(vkey)
	if err != nil {
		return nil, fmt.Errorf("failed to create verifier: %w", err)
	}
	if len(origin) == 0 {
		origin = verifier.Name()
		klog.Infof("No origin provided; using verifier name: %q", origin)
	}
	return &tLogTilesLogClient{
		url:      lr,
		origin:   origin,
		verifier: verifier,
		fetcher: func(ctx context.Context, path string) ([]byte, error) {
			return fetcher(ctx, path)
		},
	}, nil
}

type tLogTilesLogClient struct {
	url      string
	origin   string
	verifier note.Verifier
	fetcher  tiles_client.Fetcher
}

func (c *tLogTilesLogClient) GetLogType() string {
	return "tiles"
}

func (c *tLogTilesLogClient) GetURL() string {
	return c.url
}

func (c *tLogTilesLogClient) GetOrigin() string {
	return c.origin
}

func (c *tLogTilesLogClient) GetVerifier() note.Verifier {
	return c.verifier
}

func (c *tLogTilesLogClient) GetCheckpoint() (*model.Checkpoint, error) {
	cp, raw, n, err := tiles_client.FetchCheckpoint(context.Background(), c.fetcher, c.verifier, c.origin)
	return &model.Checkpoint{
		Checkpoint: cp,
		Raw:        raw,
		Note:       n,
	}, err
}

func (c *tLogTilesLogClient) GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error) {
	if checkpoint == nil {
		return nil, errors.New("checkpoint is nil")
	}
	if index >= checkpoint.Size {
		return nil, fmt.Errorf("index %d out of bounds for checkpoint size %d", index, checkpoint.Size)
	}
	bundleIndex := index / 256
	leafOffset := index % 256
	// TODO(mhutchinson): cache the bundle so consecutive leaf fetching is efficient
	bundle, err := tiles_client.GetEntryBundle(context.Background(), c.fetcher, bundleIndex, checkpoint.Size)
	if err != nil {
		return nil, err
	}
	leaf := bundle.Entries[leafOffset]

	pb, err := tiles_client.NewProofBuilder(context.Background(), *checkpoint.Checkpoint, c.fetcher)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof builder: %w", err)
	}
	incProof, err := pb.InclusionProof(context.Background(), index)
	if err != nil {
		return nil, fmt.Errorf("failed to build inclusion proof: %w", err)
	}

	h := rfc6962.DefaultHasher
	leafHash := h.HashLeaf(leaf)
	if err := proof.VerifyInclusion(h, index, checkpoint.Size, leafHash, incProof, checkpoint.Hash); err != nil {
		return nil, fmt.Errorf("failed to verify inclusion proof: %w", err)
	}

	return leaf, nil
}

func (c *tLogTilesLogClient) FormatLeaf(leaf []byte) string {
	return string(leaf)
}

func newServerlessLogClient(lr string, origin string, vkey string) (logClient, error) {
	if !strings.HasSuffix(lr, "/") {
		lr = lr + "/"
	}
	logRoot, err := url.Parse(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL %q: %w", lr, err)
	}
	fetcher := newFetcher(logRoot)
	verifier, err := note.NewVerifier(vkey)
	if err != nil {
		return nil, fmt.Errorf("failed to create verifier: %w", err)
	}
	return &serverlessLogClient{
		url:      lr,
		origin:   origin,
		verifier: verifier,
		fetcher:  fetcher,
	}, nil
}

type serverlessLogClient struct {
	url      string
	origin   string
	verifier note.Verifier
	fetcher  serverless_client.Fetcher
}

func (c *serverlessLogClient) GetLogType() string {
	return "serverless"
}

func (c *serverlessLogClient) GetURL() string {
	return c.url
}

func (c *serverlessLogClient) GetOrigin() string {
	return c.origin
}

func (c *serverlessLogClient) GetVerifier() note.Verifier {
	return c.verifier
}

func (c *serverlessLogClient) GetCheckpoint() (*model.Checkpoint, error) {
	cp, raw, n, err := serverless_client.FetchCheckpoint(context.Background(), c.fetcher, c.verifier, c.origin)
	return &model.Checkpoint{
		Checkpoint: cp,
		Raw:        raw,
		Note:       n,
	}, err
}

func (c *serverlessLogClient) GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error) {
	if checkpoint == nil {
		return nil, errors.New("checkpoint is nil")
	}
	if index >= checkpoint.Size {
		return nil, fmt.Errorf("index %d out of bounds for checkpoint size %d", index, checkpoint.Size)
	}
	leaf, err := serverless_client.GetLeaf(context.Background(), c.fetcher, index)
	if err != nil {
		return nil, err
	}

	h := rfc6962.DefaultHasher
	pb, err := serverless_client.NewProofBuilder(context.Background(), *checkpoint.Checkpoint, h.HashChildren, c.fetcher)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof builder: %w", err)
	}
	incProof, err := pb.InclusionProof(context.Background(), index)
	if err != nil {
		return nil, fmt.Errorf("failed to build inclusion proof: %w", err)
	}

	leafHash := h.HashLeaf(leaf)
	if err := proof.VerifyInclusion(h, index, checkpoint.Size, leafHash, incProof, checkpoint.Hash); err != nil {
		return nil, fmt.Errorf("failed to verify inclusion proof: %w", err)
	}

	return leaf, nil
}

func (c *serverlessLogClient) FormatLeaf(leaf []byte) string {
	return string(leaf)
}

func newSumDBLogClient(lr string, origin string, vkey string) (logClient, error) {
	logRoot, err := url.Parse(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL %q: %w", lr, err)
	}
	fetcher := newFetcher(logRoot)
	verifier, err := note.NewVerifier(vkey)
	if err != nil {
		return nil, fmt.Errorf("failed to create verifier: %w", err)
	}
	return &sumDBLogClient{
		url:      lr,
		origin:   origin,
		verifier: verifier,
		fetcher:  fetcher,
	}, nil
}

type sumDBLogClient struct {
	url      string
	origin   string
	verifier note.Verifier
	fetcher  serverless_client.Fetcher
}

func (c *sumDBLogClient) GetLogType() string {
	return "sumdb"
}

func (c *sumDBLogClient) GetURL() string {
	return c.url
}

func (c *sumDBLogClient) GetOrigin() string {
	return c.origin
}

func (c *sumDBLogClient) GetVerifier() note.Verifier {
	return c.verifier
}

func (c *sumDBLogClient) GetCheckpoint() (*model.Checkpoint, error) {
	cpRaw, err := c.fetcher(context.Background(), "/latest")
	if err != nil {
		return nil, err
	}

	cp, _, n, err := log.ParseCheckpoint(cpRaw, c.origin, c.verifier)
	return &model.Checkpoint{
		Checkpoint: cp,
		Raw:        cpRaw,
		Note:       n,
	}, err
}

func (c *sumDBLogClient) Height() int {
	return 8
}

func (c *sumDBLogClient) ReadTiles(tiles []tlog.Tile) ([][]byte, error) {
	var data [][]byte
	for _, t := range tiles {
		if t.L < 0 {
			return nil, fmt.Errorf("unexpected data tile request in ReadTiles: %v", t)
		}
		path := "/" + t.Path()
		b, err := c.fetcher(context.Background(), path)
		if err != nil {
			return nil, err
		}
		data = append(data, b)
	}
	return data, nil
}

func (c *sumDBLogClient) SaveTiles(tiles []tlog.Tile, data [][]byte) {
	// no-op
}

func (c *sumDBLogClient) GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error) {
	if checkpoint == nil {
		return nil, errors.New("checkpoint is nil")
	}
	if index >= checkpoint.Size {
		return nil, fmt.Errorf("index %d out of bounds for checkpoint size %d", index, checkpoint.Size)
	}
	const pathBase = 1000
	offset := index / 256
	nStr := fmt.Sprintf("%03d", offset%pathBase)
	for offset >= pathBase {
		offset /= pathBase
		nStr = fmt.Sprintf("x%03d/%s", offset%pathBase, nStr)
	}
	path := fmt.Sprintf("/tile/8/data/%s", nStr)
	if rem := index % 256; rem != 255 {
		path = fmt.Sprintf("%s.p/%d", path, rem+1)
	}
	data, err := c.fetcher(context.Background(), path)
	if err != nil {
		return nil, err
	}
	dataToLeaves := func(data []byte) [][]byte {
		result := make([][]byte, 0)
		start := 0
		for {
			i := bytes.Index(data[start:], []byte("\n\n"))
			if i == -1 {
				break
			}
			result = append(result, data[start:start+i+1])
			start += i + 2
		}
		result = append(result, data[start:])
		return result
	}
	leaves := dataToLeaves(data)
	leafOffset := index % 256
	if len(leaves) <= int(leafOffset) {
		return nil, fmt.Errorf("tile data truncated: expected at least %d leaves, got %d", leafOffset+1, len(leaves))
	}
	leaf := leaves[leafOffset]

	var th tlog.Hash
	copy(th[:], checkpoint.Hash)
	tree := tlog.Tree{N: int64(checkpoint.Size), Hash: th}
	hr := tlog.TileHashReader(tree, c)
	proof, err := tlog.ProveRecord(tree.N, int64(index), hr)
	if err != nil {
		return nil, fmt.Errorf("failed to prove record: %w", err)
	}

	leafHash := tlog.RecordHash(leaf)
	if err := tlog.CheckRecord(proof, tree.N, tree.Hash, int64(index), leafHash); err != nil {
		return nil, fmt.Errorf("failed to check record: %w", err)
	}

	return leaf, nil
}

func (c *sumDBLogClient) FormatLeaf(leaf []byte) string {
	return string(leaf)
}

// newFetcher creates a Fetcher for the log at the given root location.
func newFetcher(root *url.URL) serverless_client.Fetcher {
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
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
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

func parseVerifierKey(vkey string, origin string) (note.Verifier, error) {
	if v, err := tnote.NewVerifier(vkey); err == nil {
		return v, nil
	}
	if k, err := base64.StdEncoding.DecodeString(vkey); err == nil {
		if len(k) == 33 && k[0] == 0x01 {
			pubKey := ed25519.PublicKey(k[1:])
			vkeyStr, err := tnote.RFC6962VerifierString(origin, pubKey)
			if err != nil {
				return nil, err
			}
			return tnote.NewRFC6962Verifier(vkeyStr)
		}
		if pubKey, err := x509.ParsePKIXPublicKey(k); err == nil {
			vkeyStr, err := tnote.RFC6962VerifierString(origin, pubKey)
			if err != nil {
				return nil, err
			}
			return tnote.NewRFC6962Verifier(vkeyStr)
		}
	}
	return nil, fmt.Errorf("invalid verifier key format")
}

func extractPublicKey(vkey string) (crypto.PublicKey, error) {
	if parts := strings.SplitN(vkey, "+", 3); len(parts) == 3 {
		keyBytes, err := base64.StdEncoding.DecodeString(parts[2])
		if err == nil && len(keyBytes) >= 2 {
			alg := keyBytes[0]
			keyData := keyBytes[1:]
			switch alg {
			case 1: // algEd25519
				if len(keyData) == ed25519.PublicKeySize {
					return ed25519.PublicKey(keyData), nil
				}
			case 5: // algRFC6962STH
				if pubK, err := x509.ParsePKIXPublicKey(keyData); err == nil {
					return pubK, nil
				}
			}
		}
	}

	if k, err := base64.StdEncoding.DecodeString(vkey); err == nil {
		if len(k) == 33 && k[0] == 0x01 {
			return ed25519.PublicKey(k[1:]), nil
		}
		if pubKey, err := x509.ParsePKIXPublicKey(k); err == nil {
			return pubKey, nil
		}
	}
	return nil, fmt.Errorf("failed to extract public key from verifier key")
}

func newStaticCTLogClient(lr string, origin string, vkey string) (logClient, error) {
	if !strings.HasSuffix(lr, "/") {
		lr = lr + "/"
	}

	isRawKey := false
	if _, err := base64.StdEncoding.DecodeString(vkey); err == nil && !strings.Contains(vkey, "+") {
		isRawKey = true
	}
	if isRawKey && len(origin) == 0 {
		return nil, fmt.Errorf("origin must be provided when using raw base64 verifier key")
	}

	verifier, err := parseVerifierKey(vkey, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to parse verifier key: %w", err)
	}
	if len(origin) == 0 {
		origin = verifier.Name()
	}

	pubK, err := extractPublicKey(vkey)
	if err != nil {
		return nil, fmt.Errorf("failed to extract public key: %w", err)
	}

	client, err := sunlight.NewClient(&sunlight.ClientConfig{
		MonitoringPrefix: lr,
		PublicKey:        pubK,
		UserAgent:        "woodpecker/0.1.0 (+https://github.com/mhutchinson/woodpecker)",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sunlight client: %w", err)
	}

	return &staticCTLogClient{
		url:      lr,
		origin:   origin,
		verifier: verifier,
		client:   client,
	}, nil
}

type staticCTLogClient struct {
	url      string
	origin   string
	verifier note.Verifier
	client   *sunlight.Client

	sfg singleflight.Group
}

func (c *staticCTLogClient) GetLogType() string {
	return "static-ct"
}

func (c *staticCTLogClient) GetURL() string {
	return c.url
}

func (c *staticCTLogClient) GetOrigin() string {
	return c.origin
}

func (c *staticCTLogClient) GetVerifier() note.Verifier {
	return c.verifier
}

func (c *staticCTLogClient) GetCheckpoint() (*model.Checkpoint, error) {
	val, err, _ := c.sfg.Do("checkpoint", func() (interface{}, error) {
		cp, n, err := c.client.Checkpoint(context.Background())
		if err != nil {
			return nil, err
		}

		var sb strings.Builder
		sb.WriteString(n.Text)
		if !strings.HasSuffix(n.Text, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		for _, sig := range n.Sigs {
			fmt.Fprintf(&sb, "\u2014 %s %s\n", sig.Name, sig.Base64)
		}
		for _, sig := range n.UnverifiedSigs {
			fmt.Fprintf(&sb, "\u2014 %s %s\n", sig.Name, sig.Base64)
		}

		return &model.Checkpoint{
			Checkpoint: &log.Checkpoint{
				Origin: cp.Origin,
				Size:   uint64(cp.N),
				Hash:   cp.Hash[:],
			},
			Note: n,
			Raw:  []byte(sb.String()),
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*model.Checkpoint), nil
}

func (c *staticCTLogClient) GetLeaf(checkpoint *model.Checkpoint, index uint64) ([]byte, error) {
	if checkpoint == nil {
		return nil, errors.New("checkpoint is nil")
	}
	if index >= checkpoint.Size {
		return nil, fmt.Errorf("index %d out of bounds for checkpoint size %d", index, checkpoint.Size)
	}
	var th tlog.Hash
	copy(th[:], checkpoint.Hash)
	tree := tlog.Tree{N: int64(checkpoint.Size), Hash: th}

	entry, _, err := c.client.Entry(context.Background(), tree, int64(index))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch entry %d: %w", index, err)
	}

	return json.Marshal(entry)
}

func (c *staticCTLogClient) FormatLeaf(leaf []byte) string {
	var entry struct {
		Certificate    []byte
		IsPrecert      bool
		PreCertificate []byte
	}
	if err := json.Unmarshal(leaf, &entry); err != nil {
		cert, err := x509.ParseCertificate(leaf)
		if err != nil {
			return string(leaf)
		}
		return formatCert(cert)
	}

	certBytes := entry.Certificate
	if entry.IsPrecert {
		certBytes = entry.PreCertificate
	}
	if len(certBytes) == 0 {
		return string(leaf)
	}
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return fmt.Sprintf("Failed to parse cert: %v", err)
	}
	return formatCert(cert)
}

func formatCert(cert *x509.Certificate) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Subject: %s\n", cert.Subject)
	fmt.Fprintf(&sb, "Issuer: %s\n", cert.Issuer)
	fmt.Fprintf(&sb, "Serial Number: %s\n", cert.SerialNumber)
	fmt.Fprintf(&sb, "Not Before: %s\n", cert.NotBefore.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Not After: %s\n", cert.NotAfter.Format(time.RFC3339))
	if len(cert.DNSNames) > 0 {
		sb.WriteString("DNS Names:\n")
		for _, name := range cert.DNSNames {
			fmt.Fprintf(&sb, "  - %s\n", name)
		}
	}
	return sb.String()
}
