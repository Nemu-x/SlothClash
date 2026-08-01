package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// makeTarGz builds an in-memory .tar.gz from name->content and returns it plus
// its hex sha256 — the same shape our CI bundle has.
func makeTarGz(t *testing.T, files map[string]string) (archive []byte, sha string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:])
}

func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
}

func TestEnsureComponentInstallsVerifiesAndCaches(t *testing.T) {
	archive, hash := makeTarGz(t, map[string]string{
		"openconnect":    "#!/bin/sh\necho hi\n",
		"vpnc-script":    "#!/bin/sh\n",
		"libs/foo.dylib": "binary-ish",
	})
	srv := serve(t, archive)
	defer srv.Close()

	root := t.TempDir()
	spec := componentSpec{
		Name:    "openconnect",
		Version: "9.21-test",
		URL:     srv.URL,
		SHA256:  hash,
		BinRel:  "openconnect",
	}

	bin, err := ensureComponentAt(context.Background(), root, spec)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("executable not installed: %v", err)
	}
	if !componentInstalledAt(root, spec) {
		t.Fatal("should report installed after ensure")
	}

	// Second call must NOT re-download — point at a dead URL and expect success
	// because the pinned version is already present.
	cached := spec
	cached.URL = "http://127.0.0.1:0/should-not-be-hit"
	if _, err := ensureComponentAt(context.Background(), root, cached); err != nil {
		t.Fatalf("already-installed ensure should not download: %v", err)
	}
}

func TestEnsureComponentRejectsBadHashFailClosed(t *testing.T) {
	archive, _ := makeTarGz(t, map[string]string{"openconnect": "payload"})
	srv := serve(t, archive)
	defer srv.Close()

	root := t.TempDir()
	spec := componentSpec{
		Name:    "openconnect",
		Version: "9.21-test",
		URL:     srv.URL,
		SHA256:  strings.Repeat("0", 64), // valid shape, wrong value
		BinRel:  "openconnect",
	}
	if _, err := ensureComponentAt(context.Background(), root, spec); err == nil {
		t.Fatal("expected a hash-mismatch error")
	}
	if componentInstalledAt(root, spec) {
		t.Fatal("must not install a component that failed verification")
	}
}

func TestEnsureComponentRejectsMissingBinary(t *testing.T) {
	// Archive verifies fine but does not contain the declared executable.
	archive, hash := makeTarGz(t, map[string]string{"README": "no binary here"})
	srv := serve(t, archive)
	defer srv.Close()

	root := t.TempDir()
	spec := componentSpec{
		Name: "openconnect", Version: "v", URL: srv.URL, SHA256: hash, BinRel: "openconnect",
	}
	if _, err := ensureComponentAt(context.Background(), root, spec); err == nil {
		t.Fatal("expected error when the declared binary is absent")
	}
	if componentInstalledAt(root, spec) {
		t.Fatal("must not mark installed without the binary")
	}
}

func TestUntarGzRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = gz.Close()

	if err := untarGz(buf.Bytes(), t.TempDir()); err == nil {
		t.Fatal("expected zip-slip rejection for a ../ entry")
	}
}
