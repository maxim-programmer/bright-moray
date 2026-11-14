package auth

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestRefreshToken(t *testing.T) {
	tok, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != refreshTokenLen {
		t.Fatalf("длина токена %d, ожидали %d", len(tok), refreshTokenLen)
	}
	if len(hash) != 32 {
		t.Fatalf("длина хеша %d, ожидали 32", len(hash))
	}
	sum := sha256.Sum256([]byte(tok))
	if !bytes.Equal(hash, sum[:]) {
		t.Fatal("хеш не равен SHA-256 токена")
	}
	if bytes.Contains(hash, []byte(tok)) {
		t.Fatal("хеш содержит токен")
	}
	again, ok := HashRefreshToken(tok)
	if !ok || !bytes.Equal(again, hash) {
		t.Fatal("HashRefreshToken не воспроизводит хеш")
	}
}

func TestRefreshTokenUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok, _, _ := NewRefreshToken()
		if seen[tok] {
			t.Fatal("токены повторились")
		}
		seen[tok] = true
	}
}

func TestHashRefreshTokenRejectsForeign(t *testing.T) {
	good, _, _ := NewRefreshToken()
	for _, s := range []string{
		"", "short", good + "A", good[:len(good)-1],
		"!" + good[1:],  // символ вне алфавита
		good[:42] + "=", // заполнение base64
		good[:20] + " " + good[21:],
	} {
		if _, ok := HashRefreshToken(s); ok {
			t.Errorf("%q принят", s)
		}
	}
}
