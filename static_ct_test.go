package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/sunlight"
	tnote "github.com/transparency-dev/formats/note"
	"golang.org/x/mod/sumdb/tlog"
)

func TestParseVerifierKey(t *testing.T) {
	// 1. Valid key with 0x01 prefix (Static CT style)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	keyBytes := append([]byte{0x01}, pub...)
	vkey := base64.StdEncoding.EncodeToString(keyBytes)

	v, err := parseVerifierKey(vkey, "example.com")
	if err != nil {
		t.Errorf("failed to parse valid 0x01 prefixed key: %v", err)
	} else if v.Name() != "example.com" {
		t.Errorf("expected verifier name to be %q, got %q", "example.com", v.Name())
	}

	// 2. Fallback key (Standard note verifier)
	sumdbKey := "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
	vFallback, err := parseVerifierKey(sumdbKey, "sum.golang.org")
	if err != nil {
		t.Errorf("failed to parse fallback note key: %v", err)
	} else if vFallback.Name() != "sum.golang.org" {
		t.Errorf("expected fallback verifier name to be %q, got %q", "sum.golang.org", vFallback.Name())
	}

	// 3. Invalid key (malformed)
	invalidKey := "invalid-key-format-without-plus"
	_, err = parseVerifierKey(invalidKey, "example.com")
	if err == nil {
		t.Error("expected error parsing invalid key, but got nil")
	}
}

func generateSelfSignedCert(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(12345),
		Subject: pkix.Name{
			CommonName: "woodpecker.test",
		},
		Issuer: pkix.Name{
			CommonName: "woodpecker.test.issuer",
		},
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		DNSNames:  []string{"woodpecker.test", "www.woodpecker.test"},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, priv.Public(), priv)
	if err != nil {
		t.Fatalf("failed to create self-signed cert: %v", err)
	}
	return certBytes
}

func TestFormatLeaf(t *testing.T) {
	certBytes := generateSelfSignedCert(t)
	client := &staticCTLogClient{origin: "test-log"}

	// 1. Test direct DER parsing (fallback)
	resDirect := client.FormatLeaf(certBytes)
	if !strings.Contains(resDirect, "Subject: CN=woodpecker.test") {
		t.Errorf("expected Subject in formatted leaf, got:\n%s", resDirect)
	}
	if !strings.Contains(resDirect, "Issuer: CN=woodpecker.test") {
		t.Errorf("expected Issuer in formatted leaf, got:\n%s", resDirect)
	}
	if !strings.Contains(resDirect, "Serial Number: 12345") {
		t.Errorf("expected Serial Number in formatted leaf, got:\n%s", resDirect)
	}
	if !strings.Contains(resDirect, "DNS Names:\n  - woodpecker.test\n  - www.woodpecker.test") {
		t.Errorf("expected DNS Names in formatted leaf, got:\n%s", resDirect)
	}

	// 2. Test JSON entry parsing (non-precert)
	entryJSON, err := json.Marshal(struct {
		Certificate    []byte
		IsPrecert      bool
		PreCertificate []byte
	}{
		Certificate: certBytes,
	})
	if err != nil {
		t.Fatalf("failed to marshal entry: %v", err)
	}

	resJSON := client.FormatLeaf(entryJSON)
	if !strings.Contains(resJSON, "Subject: CN=woodpecker.test") {
		t.Errorf("expected Subject in formatted leaf (JSON), got:\n%s", resJSON)
	}

	// 3. Test JSON entry parsing (precert)
	precertJSON, err := json.Marshal(struct {
		Certificate    []byte
		IsPrecert      bool
		PreCertificate []byte
	}{
		IsPrecert:      true,
		PreCertificate: certBytes,
	})
	if err != nil {
		t.Fatalf("failed to marshal entry: %v", err)
	}

	resPrecert := client.FormatLeaf(precertJSON)
	if !strings.Contains(resPrecert, "Subject: CN=woodpecker.test") {
		t.Errorf("expected Subject in formatted leaf (precert JSON), got:\n%s", resPrecert)
	}

	// 4. Test malformed input handling
	malformed := []byte("definitely-not-a-certificate-or-json")
	resMalformed := client.FormatLeaf(malformed)
	if resMalformed != "definitely-not-a-certificate-or-json" {
		t.Errorf("expected malformed input to be returned as-is, got %q", resMalformed)
	}
}

func signSTH(priv *ecdsa.PrivateKey, logName string, treeSize uint64, rootHash [32]byte, timestamp uint64) ([]byte, error) {
	b := make([]byte, 2+8+8+32)
	b[0] = 0 // Version V1 = 0
	b[1] = 1 // TreeHashSignatureType = 1
	binary.BigEndian.PutUint64(b[2:], timestamp)
	binary.BigEndian.PutUint64(b[10:], treeSize)
	copy(b[18:], rootHash[:])

	dgst := sha256.Sum256(b)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, dgst[:])
	if err != nil {
		return nil, err
	}

	// TLS digitally-signed struct
	tlsSig := make([]byte, 0, 4+len(sig))
	tlsSig = append(tlsSig, 4, 3) // sha256, ecdsa
	tlsSig = binary.BigEndian.AppendUint16(tlsSig, uint16(len(sig)))
	tlsSig = append(tlsSig, sig...)

	return tlsSig, nil
}

func TestStaticCTLogClient(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	pubKeyDer, err := x509.MarshalPKIXPublicKey(privKey.Public())
	if err != nil {
		t.Fatalf("failed to marshal PKIX public key: %v", err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyDer)

	logName := "woodpecker.test/log"
	timestamp := uint64(time.Now().UnixMilli())
	certBytes := generateSelfSignedCert(t)

	// Create LogEntry and compute root hash
	entry := &sunlight.LogEntry{
		Certificate: certBytes,
		IsPrecert:   false,
		Timestamp:   int64(timestamp),
	}
	leafBytes := entry.MerkleTreeLeaf()
	rootHash := tlog.RecordHash(leafBytes)

	// Sign STH
	tlsSig, err := signSTH(privKey, logName, 1, rootHash, timestamp)
	if err != nil {
		t.Fatalf("failed to sign STH: %v", err)
	}

	// Format note checkpoint
	// Note verifier needs to be created
	vkeyStr, err := tnote.RFC6962VerifierString(logName, privKey.Public())
	if err != nil {
		t.Fatalf("failed to create verifier string: %v", err)
	}
	v, err := tnote.NewRFC6962Verifier(vkeyStr)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	sigBytes := binary.BigEndian.AppendUint32(nil, v.KeyHash())
	sigBytes = binary.BigEndian.AppendUint64(sigBytes, timestamp)
	sigBytes = append(sigBytes, tlsSig...)

	sigLine := fmt.Sprintf("\u2014 %s %s", logName, base64.StdEncoding.EncodeToString(sigBytes))
	noteText := fmt.Sprintf("%s\n%d\n%s\n", logName, 1, base64.StdEncoding.EncodeToString(rootHash[:]))
	signedCheckpoint := fmt.Sprintf("%s\n%s\n", noteText, sigLine)

	// Setup mock HTTP server
	tileBytes := sunlight.AppendTileLeaf(nil, entry)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checkpoint":
			w.Header().Set("Content-Type", "text/plain")
			if _, err := w.Write([]byte(signedCheckpoint)); err != nil {
				t.Errorf("failed to write checkpoint: %v", err)
			}
		case "/tile/data/000.p/1":
			w.Header().Set("Content-Type", "application/octet-stream")
			if _, err := w.Write(tileBytes); err != nil {
				t.Errorf("failed to write tile: %v", err)
			}
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := newStaticCTLogClient(ts.URL, logName, pubKeyB64)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test GetOrigin
	if client.GetOrigin() != logName {
		t.Errorf("expected origin %q, got %q", logName, client.GetOrigin())
	}

	// Test GetCheckpoint
	cp, err := client.GetCheckpoint()
	if err != nil {
		t.Fatalf("failed to get checkpoint: %v", err)
	}
	if cp.Size != 1 {
		t.Errorf("expected checkpoint size 1, got %d", cp.Size)
	}

	// Test GetLeaf
	leafBytesRet, err := client.GetLeaf(1, 0)
	if err != nil {
		t.Fatalf("failed to get leaf: %v", err)
	}

	// Leaf should be formatted correctly
	formatted := client.FormatLeaf(leafBytesRet)
	if !strings.Contains(formatted, "Subject: CN=woodpecker.test") {
		t.Errorf("expected Subject in formatted leaf, got:\n%s", formatted)
	}
}

func TestNewStaticCTLogClientEmptyOriginRawKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	keyBytes := append([]byte{0x01}, pub...)
	vkey := base64.StdEncoding.EncodeToString(keyBytes)

	_, err = newStaticCTLogClient("http://example.com", "", vkey)
	if err == nil {
		t.Error("expected error when initializing raw key with empty origin, but got nil")
	}
}

func TestFormatLeafNonCertJSON(t *testing.T) {
	client := &staticCTLogClient{origin: "test-log"}
	nonCertJSON := []byte(`{"some_key": "some_value"}`)
	res := client.FormatLeaf(nonCertJSON)
	if res != string(nonCertJSON) {
		t.Errorf("expected non-cert JSON to return as-is, got %q", res)
	}
}

func TestStaticCTLogClientConcurrency(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}

	pubKeyDer, err := x509.MarshalPKIXPublicKey(privKey.Public())
	if err != nil {
		t.Fatalf("failed to marshal PKIX public key: %v", err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyDer)

	logName := "woodpecker.test/log"
	timestamp := uint64(time.Now().UnixMilli())
	certBytes := generateSelfSignedCert(t)

	entry := &sunlight.LogEntry{
		Certificate: certBytes,
		IsPrecert:   false,
		Timestamp:   int64(timestamp),
	}
	leafBytes := entry.MerkleTreeLeaf()
	rootHash := tlog.RecordHash(leafBytes)

	tlsSig, err := signSTH(privKey, logName, 1, rootHash, timestamp)
	if err != nil {
		t.Fatalf("failed to sign STH: %v", err)
	}

	vkeyStr, err := tnote.RFC6962VerifierString(logName, privKey.Public())
	if err != nil {
		t.Fatalf("failed to create verifier string: %v", err)
	}
	v, err := tnote.NewRFC6962Verifier(vkeyStr)
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}

	sigBytes := binary.BigEndian.AppendUint32(nil, v.KeyHash())
	sigBytes = binary.BigEndian.AppendUint64(sigBytes, timestamp)
	sigBytes = append(sigBytes, tlsSig...)

	sigLine := fmt.Sprintf("\u2014 %s %s", logName, base64.StdEncoding.EncodeToString(sigBytes))
	noteText := fmt.Sprintf("%s\n%d\n%s\n", logName, 1, base64.StdEncoding.EncodeToString(rootHash[:]))
	signedCheckpoint := fmt.Sprintf("%s\n%s\n", noteText, sigLine)
	tileBytes := sunlight.AppendTileLeaf(nil, entry)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checkpoint":
			w.Header().Set("Content-Type", "text/plain")
			if _, err := w.Write([]byte(signedCheckpoint)); err != nil {
				t.Errorf("failed to write checkpoint: %v", err)
			}
		case "/tile/data/000.p/1":
			w.Header().Set("Content-Type", "application/octet-stream")
			if _, err := w.Write(tileBytes); err != nil {
				t.Errorf("failed to write tile: %v", err)
			}
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := newStaticCTLogClient(ts.URL, logName, pubKeyB64)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	workers := 10
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if _, err := client.GetCheckpoint(); err != nil {
						t.Errorf("GetCheckpoint error: %v", err)
						return
					}
				}
			}
		}()
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if _, err := client.GetLeaf(1, 0); err != nil {
						t.Errorf("GetLeaf error: %v", err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
