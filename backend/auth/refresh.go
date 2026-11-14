package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

const (
	refreshTokenBytes = 32 // 256 бит энтропии
	refreshTokenLen   = 43 // длина base64url (без заполнения) от 32 байт
)

// NewRefreshToken создаёт случайный refresh-токен. Клиенту отдаётся token,
// в БД сохраняется только hash (SHA-256).
func NewRefreshToken() (token string, hash []byte, err error) {
	var raw [refreshTokenBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, fmt.Errorf("auth: источник случайности: %w", err)
	}
	token = b64.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}

// HashRefreshToken возвращает хеш предъявленного токена. ok=false — строка не может
// быть нашим токеном (неверная длина или алфавит): в БД с ней ходить не нужно.
func HashRefreshToken(token string) (hash []byte, ok bool) {
	if len(token) != refreshTokenLen {
		return nil, false
	}
	if _, err := b64.DecodeString(token); err != nil {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}
