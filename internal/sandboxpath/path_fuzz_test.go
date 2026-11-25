package sandboxpath

import (
	"path"
	"strings"
	"testing"
)

func FuzzValidatePath(f *testing.F) {
	for _, seed := range []string{
		"/tmp/file.txt",
		"work/nested",
		"/",
		".",
		"..",
		"../etc/passwd",
		"/a/b/../../..",
		"a/\x00b",
		"   ",
		"a/./b/../c",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		err := Validate(raw)
		if err == nil {
			if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, 0) {
				t.Fatalf("Validate(%q) accepted empty or NUL path", raw)
			}
			clean := path.Clean("/" + raw)
			if clean == ".." || strings.HasPrefix(clean, "../") {
				t.Fatalf("Validate(%q) allowed traversal clean=%q", raw, clean)
			}
		}
	})
}
