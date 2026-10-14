package auth

import "testing"

func TestGenerateAndVerify(t *testing.T) {
	k, err := Generate("key_1", "test", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Prefix) != 8 {
		t.Fatalf("prefix length=%d", len(k.Prefix))
	}
	if !Verify(k.Secret, k.Hash) {
		t.Fatal("generated key did not verify")
	}
	if Verify(k.Secret+"x", k.Hash) {
		t.Fatal("modified key verified")
	}
	prefix, digest, err := Parse(k.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if prefix != k.Prefix || string(digest) != string(k.Hash) {
		t.Fatal("parsed key did not match generated key")
	}
}
