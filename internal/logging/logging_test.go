package logging

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestRedactsSensitiveAttributes(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = write
	New("info", "json").Info("request", "api_key", "cbx_secret", "safe", "ok")
	_ = write.Close()
	os.Stderr = old
	b, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "cbx_secret") || !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("unexpected redaction output: %s", b)
	}
}
