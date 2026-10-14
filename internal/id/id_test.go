package id

import "testing"

func TestNewPrefixedSortableID(t *testing.T) {
	a, e := New("ses")
	if e != nil {
		t.Fatal(e)
	}
	b, e := New("ses")
	if e != nil {
		t.Fatal(e)
	}
	if len(a) != 30 || a[:4] != "ses_" || len(b) != 30 || b[:4] != "ses_" || a == b {
		t.Fatalf("unexpected ids %q %q", a, b)
	}
}
