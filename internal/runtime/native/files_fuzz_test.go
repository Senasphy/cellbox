//go:build linux

package native

import (
	"archive/tar"
	"bytes"
	"testing"
)

func FuzzValidateNativeArchive(f *testing.F) {
	var valid bytes.Buffer
	tw := tar.NewWriter(&valid)
	_ = tw.WriteHeader(&tar.Header{Name: "dir/file.txt", Mode: 0644, Size: 5, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("hello"))
	_ = tw.Close()
	f.Add(valid.Bytes())

	var traversal bytes.Buffer
	tw = tar.NewWriter(&traversal)
	_ = tw.WriteHeader(&tar.Header{Name: "../../etc/passwd", Mode: 0644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	f.Add(traversal.Bytes())

	f.Add([]byte("not-a-tar"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = validateNativeArchive(bytes.NewReader(data))
	})
}
