package adminauth

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	password := GeneratePassword()

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == password {
		t.Fatal("hash equals password")
	}
	if !CheckPassword(hash, password) {
		t.Error("correct password rejected")
	}
	if CheckPassword(hash, password+"x") {
		t.Error("wrong password accepted")
	}
}

func TestSessionToken(t *testing.T) {
	token := GenerateSessionToken()

	if !strings.HasPrefix(token, SessionPrefix) {
		t.Errorf("token %q does not start with %q", token, SessionPrefix)
	}
	if GenerateSessionToken() == token {
		t.Error("two tokens are equal")
	}
	if len(HashToken(token)) != 32 {
		t.Error("hash is not 32 bytes")
	}
}
