// Package auth — подписанные сессионные токены (HMAC-SHA256).
// Формат: base64url("<userID>.<expUnix>") + "." + base64url(hmac).
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid token")

var enc = base64.RawURLEncoding

func sign(secret []byte, payload string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func Sign(secret []byte, userID int64, ttl time.Duration) string {
	payload := fmt.Sprintf("%d.%d", userID, time.Now().Add(ttl).Unix())
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(sign(secret, payload))
}

func Parse(secret []byte, token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return 0, ErrInvalidToken
	}
	payloadRaw, err := enc.DecodeString(parts[0])
	if err != nil {
		return 0, ErrInvalidToken
	}
	got, err := enc.DecodeString(parts[1])
	if err != nil {
		return 0, ErrInvalidToken
	}
	payload := string(payloadRaw)
	if !hmac.Equal(got, sign(secret, payload)) {
		return 0, ErrInvalidToken
	}
	fields := strings.Split(payload, ".")
	if len(fields) != 2 {
		return 0, ErrInvalidToken
	}
	uid, err1 := strconv.ParseInt(fields[0], 10, 64)
	exp, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil || uid <= 0 || time.Now().Unix() > exp {
		return 0, ErrInvalidToken
	}
	return uid, nil
}
