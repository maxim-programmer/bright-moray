// Package auth — токены авторизации.
//
// Access-токен — JWT (RFC 7519) с подписью HS256, живёт минуты, проверяется без обращения к БД.
// Refresh-токен — непрозрачная случайная строка; в БД хранится только её SHA-256 (см. refresh.go).
//
// JWT реализован на стандартной библиотеке: нужен ровно один алгоритм, а строгая проверка
// заголовка (никаких alg=none и подмены алгоритма) здесь проще и надёжнее, чем конфигурация
// универсальной библиотеки.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidToken — подпись, формат или claims не прошли проверку.
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken — токен подлинный, но срок его действия истёк.
	ErrExpiredToken = fmt.Errorf("%w: expired", ErrInvalidToken)
)

const (
	jwtAlg    = "HS256"
	jwtTyp    = "at+jwt" // RFC 9068: отличает access-токен от токенов другого назначения
	maxJWTLen = 2048
	// iat из будущего допускаем в пределах небольшого рассинхрона часов.
	iatLeeway = 30 * time.Second
)

var b64 = base64.RawURLEncoding.Strict()

// Options — параметры выпуска и проверки access-токенов.
type Options struct {
	Secret   []byte
	Issuer   string
	Audience string
	TTL      time.Duration
	Now      func() time.Time // подмена часов в тестах; по умолчанию time.Now
}

// Claims — проверенные данные access-токена.
type Claims struct {
	Subject   int64 // id пользователя (claim sub)
	ID        string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// JWT выпускает и проверяет access-токены.
type JWT struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
	header   string // готовый base64url заголовка
}

func NewJWT(o Options) (*JWT, error) {
	switch {
	case len(o.Secret) == 0:
		return nil, errors.New("auth: пустой секрет подписи")
	case o.Issuer == "" || o.Audience == "":
		return nil, errors.New("auth: не заданы issuer/audience")
	case o.TTL <= 0:
		return nil, errors.New("auth: TTL access-токена должен быть положительным")
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	hdr, _ := json.Marshal(jwtHeader{Alg: jwtAlg, Typ: jwtTyp})
	return &JWT{
		secret:   append([]byte(nil), o.Secret...),
		issuer:   o.Issuer,
		audience: o.Audience,
		ttl:      o.TTL,
		now:      now,
		header:   b64.EncodeToString(hdr),
	}, nil
}

// TTL — время жизни выпускаемых access-токенов.
func (j *JWT) TTL() time.Duration { return j.ttl }

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtPayload struct {
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
	Jti string `json:"jti"`
}

func (j *JWT) mac(signingInput string) []byte {
	m := hmac.New(sha256.New, j.secret)
	m.Write([]byte(signingInput))
	return m.Sum(nil)
}

// Issue выпускает access-токен пользователя.
func (j *JWT) Issue(userID int64) (string, Claims, error) {
	if userID <= 0 {
		return "", Claims{}, errors.New("auth: некорректный id пользователя")
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", Claims{}, fmt.Errorf("auth: источник случайности: %w", err)
	}
	now := j.now()
	c := Claims{
		Subject:   userID,
		ID:        b64.EncodeToString(raw[:]),
		IssuedAt:  time.Unix(now.Unix(), 0),
		ExpiresAt: time.Unix(now.Add(j.ttl).Unix(), 0),
	}
	body, err := json.Marshal(jwtPayload{
		Iss: j.issuer,
		Sub: strconv.FormatInt(userID, 10),
		Aud: j.audience,
		Exp: c.ExpiresAt.Unix(),
		Iat: c.IssuedAt.Unix(),
		Jti: c.ID,
	})
	if err != nil {
		return "", Claims{}, err
	}
	signingInput := j.header + "." + b64.EncodeToString(body)
	return signingInput + "." + b64.EncodeToString(j.mac(signingInput)), c, nil
}

// Verify проверяет формат, алгоритм, подпись и claims (iss, aud, sub, jti, iat, exp).
// Ошибки намеренно не детализируются: вызывающему важно лишь «годится / не годится».
func (j *JWT) Verify(token string) (Claims, error) {
	if token == "" || len(token) > maxJWTLen {
		return Claims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrInvalidToken
	}

	// 1. Заголовок: алгоритм и тип зашиты в код, а не берутся из токена.
	hraw, err := b64.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var h jwtHeader
	if err := json.Unmarshal(hraw, &h); err != nil || h.Alg != jwtAlg || h.Typ != jwtTyp {
		return Claims{}, ErrInvalidToken
	}

	// 2. Подпись (сравнение за постоянное время).
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, j.mac(parts[0]+"."+parts[1])) {
		return Claims{}, ErrInvalidToken
	}

	// 3. Claims.
	praw, err := b64.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var p jwtPayload
	if err := json.Unmarshal(praw, &p); err != nil {
		return Claims{}, ErrInvalidToken
	}
	uid, err := strconv.ParseInt(p.Sub, 10, 64)
	if err != nil || uid <= 0 || strconv.FormatInt(uid, 10) != p.Sub {
		return Claims{}, ErrInvalidToken
	}
	if p.Iss != j.issuer || p.Aud != j.audience || p.Jti == "" || len(p.Jti) > 64 {
		return Claims{}, ErrInvalidToken
	}
	if p.Exp <= p.Iat {
		return Claims{}, ErrInvalidToken
	}
	now := j.now()
	if time.Unix(p.Iat, 0).After(now.Add(iatLeeway)) {
		return Claims{}, ErrInvalidToken
	}
	if !now.Before(time.Unix(p.Exp, 0)) {
		return Claims{}, ErrExpiredToken
	}
	return Claims{
		Subject:   uid,
		ID:        p.Jti,
		IssuedAt:  time.Unix(p.Iat, 0),
		ExpiresAt: time.Unix(p.Exp, 0),
	}, nil
}
