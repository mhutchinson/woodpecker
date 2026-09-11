package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mhutchinson/woodpecker/model"
	"github.com/transparency-dev/formats/log"
	tnote "github.com/transparency-dev/formats/note"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

func TestExtractPublicKey(t *testing.T) {
	// 1. Note verifier string with algEd25519 (alg=1)
	_, vkeyEd, err := note.GenerateKey(rand.Reader, "ed-log")
	if err != nil {
		t.Fatalf("failed to generate ed25519 note key: %v", err)
	}
	pk1, err := extractPublicKey(vkeyEd)
	if err != nil {
		t.Fatalf("failed to extract ed25519 public key from note verifier: %v", err)
	}
	if _, ok := pk1.(ed25519.PublicKey); !ok {
		t.Errorf("expected ed25519.PublicKey, got %T", pk1)
	}

	// 2. Note verifier string with algRFC6962STH (alg=5)
	privECDSA, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ecdsa key: %v", err)
	}
	vkeyRFC, err := tnote.RFC6962VerifierString("rfc-log", &privECDSA.PublicKey)
	if err != nil {
		t.Fatalf("failed to create rfc6962 verifier string: %v", err)
	}
	pk2, err := extractPublicKey(vkeyRFC)
	if err != nil {
		t.Fatalf("failed to extract ecdsa public key from rfc6962 verifier: %v", err)
	}
	if _, ok := pk2.(*ecdsa.PublicKey); !ok {
		t.Errorf("expected *ecdsa.PublicKey, got %T", pk2)
	}

	// 3. Raw base64 with 0x01 prefix (33-byte ed25519)
	pubEd, _, _ := ed25519.GenerateKey(rand.Reader)
	rawEdB64 := base64.StdEncoding.EncodeToString(append([]byte{0x01}, pubEd...))
	pk3, err := extractPublicKey(rawEdB64)
	if err != nil {
		t.Fatalf("failed to extract ed25519 from raw 0x01 prefixed base64: %v", err)
	}
	if pubKey, ok := pk3.(ed25519.PublicKey); !ok || !pubKey.Equal(pubEd) {
		t.Errorf("extracted public key does not match original ed25519 key")
	}

	// 4. Raw base64 PKIX DER public key (ECDSA)
	der, err := x509.MarshalPKIXPublicKey(&privECDSA.PublicKey)
	if err != nil {
		t.Fatalf("failed to marshal PKIX public key: %v", err)
	}
	pkixB64 := base64.StdEncoding.EncodeToString(der)
	pk4, err := extractPublicKey(pkixB64)
	if err != nil {
		t.Fatalf("failed to extract ecdsa from PKIX base64: %v", err)
	}
	if _, ok := pk4.(*ecdsa.PublicKey); !ok {
		t.Errorf("expected *ecdsa.PublicKey, got %T", pk4)
	}

	// 5. Invalid inputs
	invalidInputs := []string{
		"",
		"not-a-valid-key",
		"prefix+name+badbase64!!!",
		base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}), // 0x01 prefix with bad length
	}
	for _, inv := range invalidInputs {
		if _, err := extractPublicKey(inv); err == nil {
			t.Errorf("expected error for invalid key %q, got nil", inv)
		}
	}
}

func TestNewFetcher_And_ReadHTTP(t *testing.T) {
	// 1. Unsupported scheme panics
	badURL, _ := url.Parse("ftp://example.com/log")
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected newFetcher to panic on unsupported scheme, but did not")
		}
	}()
	_ = newFetcher(badURL)

	// 2. File scheme works
	tmpFile, err := os.CreateTemp("", "woodpecker-fetcher-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	expectedContent := "test file content"
	if _, err := tmpFile.WriteString(expectedContent); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	_ = tmpFile.Close()

	fileRoot, _ := url.Parse("file:///")
	fileFetcher := newFetcher(fileRoot)
	data, err := fileFetcher(context.Background(), tmpFile.Name())
	if err != nil {
		t.Fatalf("file fetcher failed: %v", err)
	}
	if string(data) != expectedContent {
		t.Errorf("expected %q, got %q", expectedContent, string(data))
	}
}

func TestReadHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("http body response"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/servererror":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	// 1. 200 OK
	uSuccess, _ := url.Parse(server.URL + "/success")
	data, err := readHTTP(context.Background(), uSuccess)
	if err != nil {
		t.Fatalf("unexpected error reading HTTP 200: %v", err)
	}
	if string(data) != "http body response" {
		t.Errorf("expected 'http body response', got %q", string(data))
	}

	// 2. 404 Not Found returns os.ErrNotExist
	uNotFound, _ := url.Parse(server.URL + "/notfound")
	_, err = readHTTP(context.Background(), uNotFound)
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist on 404, got %v", err)
	}

	// 3. 500 Internal Server Error returns unexpected http status error
	uError, _ := url.Parse(server.URL + "/servererror")
	_, err = readHTTP(context.Background(), uError)
	if err == nil || !strings.Contains(err.Error(), "unexpected http status") {
		t.Errorf("expected 'unexpected http status' on 500, got %v", err)
	}
}

func TestTLogTilesLogClient(t *testing.T) {
	_, vkey, err := note.GenerateKey(rand.Reader, "tiles-origin")
	if err != nil {
		t.Fatalf("failed to generate note key: %v", err)
	}

	// 1. Constructor URL validation error
	if _, err := newTLogTilesLogClient("http://[::1]:bad_port", "origin", vkey); err == nil {
		t.Error("expected error for malformed URL, got nil")
	}

	// 2. Constructor Verifier validation error
	if _, err := newTLogTilesLogClient("http://example.com", "origin", "bad-verifier-key"); err == nil {
		t.Error("expected error for invalid verifier key, got nil")
	}

	// 3. Successful creation with empty origin using verifier name
	client, err := newTLogTilesLogClient("http://example.com/tiles", "", vkey)
	if err != nil {
		t.Fatalf("failed to create tLogTilesLogClient: %v", err)
	}
	if client.GetURL() != "http://example.com/tiles/" {
		t.Errorf("expected URL with trailing slash, got %q", client.GetURL())
	}
	if client.GetOrigin() != "tiles-origin" {
		t.Errorf("expected origin 'tiles-origin', got %q", client.GetOrigin())
	}
	if client.GetLogType() != "tiles" {
		t.Errorf("expected logType 'tiles', got %q", client.GetLogType())
	}
	if client.GetVerifier() == nil {
		t.Error("expected non-nil verifier")
	}
	if client.FormatLeaf([]byte("tile-leaf")) != "tile-leaf" {
		t.Errorf("unexpected FormatLeaf result")
	}

	// 4. GetLeaf bounds checks
	if _, err := client.GetLeaf(nil, 0); err == nil {
		t.Error("expected error for nil checkpoint in GetLeaf, got nil")
	}
	cp := &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 10}}
	if _, err := client.GetLeaf(cp, 10); err == nil {
		t.Error("expected error for index >= size in GetLeaf, got nil")
	}
}

func TestServerlessLogClient(t *testing.T) {
	_, vkey, err := note.GenerateKey(rand.Reader, "sl-origin")
	if err != nil {
		t.Fatalf("failed to generate note key: %v", err)
	}

	// 1. Constructor URL validation error
	if _, err := newServerlessLogClient("http://[::1]:bad_port", "sl-origin", vkey); err == nil {
		t.Error("expected error for malformed URL, got nil")
	}

	// 2. Constructor Verifier validation error
	if _, err := newServerlessLogClient("http://example.com", "sl-origin", "bad-verifier-key"); err == nil {
		t.Error("expected error for invalid verifier key, got nil")
	}

	// 3. Successful creation
	client, err := newServerlessLogClient("http://example.com/sl", "sl-origin", vkey)
	if err != nil {
		t.Fatalf("failed to create serverlessLogClient: %v", err)
	}
	if client.GetURL() != "http://example.com/sl/" {
		t.Errorf("expected URL with trailing slash, got %q", client.GetURL())
	}
	if client.GetOrigin() != "sl-origin" {
		t.Errorf("expected origin 'sl-origin', got %q", client.GetOrigin())
	}
	if client.GetLogType() != "serverless" {
		t.Errorf("expected logType 'serverless', got %q", client.GetLogType())
	}
	if client.GetVerifier() == nil {
		t.Error("expected non-nil verifier")
	}
	if client.FormatLeaf([]byte("sl-leaf")) != "sl-leaf" {
		t.Errorf("unexpected FormatLeaf result")
	}

	// 4. GetLeaf bounds checks
	if _, err := client.GetLeaf(nil, 0); err == nil {
		t.Error("expected error for nil checkpoint in GetLeaf, got nil")
	}
	cp := &model.Checkpoint{Checkpoint: &log.Checkpoint{Size: 5}}
	if _, err := client.GetLeaf(cp, 5); err == nil {
		t.Error("expected error for index >= size in GetLeaf, got nil")
	}
}

func TestSumDBLogClient(t *testing.T) {
	_, vkey, err := note.GenerateKey(rand.Reader, "sumdb-origin")
	if err != nil {
		t.Fatalf("failed to generate note key: %v", err)
	}

	// 1. Constructor URL validation error
	if _, err := newSumDBLogClient("http://[::1]:bad_port", "sumdb-origin", vkey); err == nil {
		t.Error("expected error for malformed URL, got nil")
	}

	// 2. Constructor Verifier validation error
	if _, err := newSumDBLogClient("http://example.com", "sumdb-origin", "bad-verifier-key"); err == nil {
		t.Error("expected error for invalid verifier key, got nil")
	}

	// 3. Successful creation
	client, err := newSumDBLogClient("http://example.com/sumdb", "sumdb-origin", vkey)
	if err != nil {
		t.Fatalf("failed to create sumDBLogClient: %v", err)
	}
	sumdbClient, ok := client.(*sumDBLogClient)
	if !ok {
		t.Fatalf("expected *sumDBLogClient, got %T", client)
	}
	if sumdbClient.GetURL() != "http://example.com/sumdb" {
		t.Errorf("expected URL 'http://example.com/sumdb', got %q", sumdbClient.GetURL())
	}
	if sumdbClient.GetOrigin() != "sumdb-origin" {
		t.Errorf("expected origin 'sumdb-origin', got %q", sumdbClient.GetOrigin())
	}
	if sumdbClient.GetLogType() != "sumdb" {
		t.Errorf("expected logType 'sumdb', got %q", sumdbClient.GetLogType())
	}
	if sumdbClient.GetVerifier() == nil {
		t.Error("expected non-nil verifier")
	}
	if sumdbClient.Height() != 8 {
		t.Errorf("expected Height() == 8, got %d", sumdbClient.Height())
	}
	if sumdbClient.FormatLeaf([]byte("sumdb-leaf")) != "sumdb-leaf" {
		t.Errorf("unexpected FormatLeaf result")
	}

	// 4. ReadTiles invalid tile level (L < 0)
	_, err = sumdbClient.ReadTiles([]tlog.Tile{{L: -1}})
	if err == nil || !strings.Contains(err.Error(), "unexpected data tile request in ReadTiles") {
		t.Errorf("expected error for data tile in ReadTiles, got %v", err)
	}

	// 5. SaveTiles is a safe no-op
	sumdbClient.SaveTiles(nil, nil)
}
