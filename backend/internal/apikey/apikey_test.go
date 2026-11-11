package apikey

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	key := Generate()

	if !strings.HasPrefix(key, Prefix) {
		t.Errorf("key %q does not start with %q", key, Prefix)
	}
	if got, want := len(key), 46; got != want {
		t.Errorf("len(key) = %d, want %d", got, want)
	}
	if other := Generate(); other == key {
		t.Error("two generated keys are equal")
	}
}

func TestHash(t *testing.T) {
	key := Generate()
	h1 := Hash(key)
	h2 := Hash(key)

	if len(h1) != 32 {
		t.Errorf("len(hash) = %d, want 32", len(h1))
	}
	if !bytes.Equal(h1, h2) {
		t.Error("hash of the same key differs between calls")
	}
	if bytes.Equal(h1, Hash(Generate())) {
		t.Error("hash of different keys is equal")
	}
}

func TestDisplayPrefix(t *testing.T) {
	key := "dg_abcdefghijk"

	if got, want := DisplayPrefix(key), "dg_abcde"; got != want {
		t.Errorf("DisplayPrefix(%q) = %q, want %q", key, got, want)
	}
}
