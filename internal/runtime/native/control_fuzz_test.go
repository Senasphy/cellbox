//go:build linux

package native

import (
	"testing"

	"golang.org/x/sys/unix"
)

func FuzzDecodeControlPacket(f *testing.F) {
	f.Add([]byte(`{"type":"ready","id":"ses_1"}`), []byte{}, 0)
	f.Add([]byte(`{"type":"exec","id":"ex_1","payload":{"argv":["echo"]}}`), []byte{1, 2, 3, 4}, unix.MSG_TRUNC)
	f.Add([]byte(`{bad`), []byte{}, 0)
	f.Add([]byte{}, []byte{}, 0)

	f.Fuzz(func(t *testing.T, data, oob []byte, flags int) {
		msg, files, err := decodeControlPacket(data, oob, flags)
		closeFiles(files)
		if err == nil && flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			t.Fatalf("decodeControlPacket accepted truncated packet: %+v", msg)
		}
	})
}
