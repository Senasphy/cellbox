package id

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

var encoding = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// New returns a sortable, prefixed ULID using only the standard library.
func New(kind string) (string, error) {
	if kind == "" || strings.ContainsAny(kind, "_ /\\") {
		return "", fmt.Errorf("invalid id kind %q", kind)
	}
	var raw [16]byte
	ms := uint64(time.Now().UTC().UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", fmt.Errorf("generate id entropy: %w", err)
	}
	return kind + "_" + encoding.EncodeToString(raw[:]), nil
}
