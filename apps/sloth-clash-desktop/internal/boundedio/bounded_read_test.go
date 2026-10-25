package boundedio

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestReadLimitErrorIsDetectableThroughWrapping(t *testing.T) {
	_, err := Read(strings.NewReader("12345"), "GET /x", 4)
	if err == nil || err.Error() != "GET /x response exceeds the 4 bytes limit" {
		t.Fatalf("oversized body: %v", err)
	}
	if !IsLimitError(err) || !IsLimitError(fmt.Errorf("refresh: %w", err)) {
		t.Fatal("limit error not detectable")
	}
	if IsLimitError(io.ErrUnexpectedEOF) || IsLimitError(nil) {
		t.Fatal("unrelated error classified as a limit error")
	}
}
