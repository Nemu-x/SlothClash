package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMihomoCorpusValidFilesPassPipeline(t *testing.T) {
	t.Parallel()
	root := filepath.Clean(filepath.Join("..", "..", "tests", "corpus", "mihomo", "valid"))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read corpus dir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("valid corpus is empty: %s", root)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(root, name)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read file failed: %v", err)
			}
			doc, err := parseClashDocToMap(b)
			if err != nil {
				t.Fatalf("parseClashDocToMap failed: %v", err)
			}
			tmp := t.TempDir()
			if err := finalizeRuntimeConfigPipeline(doc, tmp, 7890, 9090, "secret", "tun", true, true); err != nil {
				t.Fatalf("finalizeRuntimeConfigPipeline failed: %v", err)
			}
		})
	}
}

func TestMihomoCorpusInvalidFilesFailParseOrFinalize(t *testing.T) {
	t.Parallel()
	root := filepath.Clean(filepath.Join("..", "..", "tests", "corpus", "mihomo", "invalid"))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read corpus dir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("invalid corpus is empty: %s", root)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(root, name)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read file failed: %v", err)
			}
			doc, pErr := parseClashDocToMap(b)
			if pErr != nil {
				return
			}
			tmp := t.TempDir()
			if err := finalizeRuntimeConfigPipeline(doc, tmp, 7890, 9090, "secret", "tun", true, true); err == nil {
				t.Fatalf("expected parse or pipeline failure for invalid corpus file")
			}
		})
	}
}

func TestLocalStressYamlFromDownloadsIfPresent(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve home dir")
	}
	p := filepath.Join(home, "Downloads", "stress.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Skip("local stress.yaml not found in Downloads")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read stress.yaml failed: %v", err)
	}
	doc, err := parseClashDocToMap(b)
	if err != nil {
		t.Fatalf("stress.yaml parse failed: %v", err)
	}
	tmp := t.TempDir()
	if err := finalizeRuntimeConfigPipeline(doc, tmp, 7890, 9090, "secret", "tun", true, true); err != nil {
		t.Fatalf("stress.yaml finalize failed: %v", err)
	}
}

