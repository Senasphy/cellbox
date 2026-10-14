// Package auth implements API key creation and verification.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// APIKey is returned at creation time; Secret must never be persisted.
type APIKey struct {
	ID     string
	Name   string
	Prefix string
	Secret string
	Hash   []byte
	Scope  string
}

// Generate creates a key in cbx_<8-character-prefix>_<base32-secret> form.
func Generate(id, name, scope string) (APIKey, error) {
	var prefixBytes [5]byte
	var secretBytes [32]byte
	if _, err := rand.Read(prefixBytes[:]); err != nil {
		return APIKey{}, err
	}
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return APIKey{}, err
	}
	prefix := b32.EncodeToString(prefixBytes[:])
	secret := b32.EncodeToString(secretBytes[:])
	return APIKey{ID: id, Name: name, Prefix: prefix, Secret: "cbx_" + prefix + "_" + secret, Hash: Hash(secret), Scope: scope}, nil
}

// Hash returns the SHA-256 digest of the encoded key secret.
func Hash(secret string) []byte { h := sha256.Sum256([]byte(secret)); return h[:] }

// Verify checks the secret component of a presented key in constant time.
func Verify(presented string, expected []byte) bool {
	parts := strings.Split(presented, "_")
	if len(parts) != 3 || parts[0] != "cbx" || len(parts[1]) != 8 || len(expected) != sha256.Size {
		return false
	}
	got := Hash(parts[2])
	return subtle.ConstantTimeCompare(got, expected) == 1
}

// Parse returns the non-secret prefix and hash for a well-formed key.
func Parse(presented string) (string, []byte, error) {
	parts := strings.Split(presented, "_")
	if len(parts) != 3 || parts[0] != "cbx" || len(parts[1]) != 8 || len(parts[2]) != 52 {
		return "", nil, errors.New("invalid API key format")
	}
	decoded, err := b32.DecodeString(parts[2])
	if err != nil || len(decoded) != 32 {
		return "", nil, errors.New("invalid API key secret")
	}
	return parts[1], Hash(parts[2]), nil
}
