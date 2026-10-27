package auth

import (
	"testing"
	"time"
)

func TestSignParse(t *testing.T) {
	secret := []byte("0123456789abcdef")
	tok := Sign(secret, 42, time.Hour)
	id, err := Parse(secret, tok)
	if err != nil || id != 42 {
		t.Fatalf("ожидали 42, получили %d, %v", id, err)
	}
	if _, err := Parse([]byte("another-secret-xx"), tok); err == nil {
		t.Fatal("токен с чужим секретом должен отклоняться")
	}
	if _, err := Parse(secret, Sign(secret, 42, -time.Minute)); err == nil {
		t.Fatal("просроченный токен должен отклоняться")
	}
	if _, err := Parse(secret, tok+"x"); err == nil {
		t.Fatal("испорченная подпись должна отклоняться")
	}
	if _, err := Parse(secret, "garbage"); err == nil {
		t.Fatal("мусор должен отклоняться")
	}
}
