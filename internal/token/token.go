package token

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/signing"
)

var ErrInvalid = errors.New("invalid token")

type Service struct {
	Config profile.Config
	Keys   map[string]signing.Signer
	Now    func() time.Time
}

type header struct {
	Typ string `json:"typ"`
	Alg string `json:"alg"`
}
type claims struct {
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Aud string `json:"aud"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

func (s Service) Issue(alg, sub string) (string, error) {
	if !profile.Supported(alg) || !s.Config.ValidSubject(sub) {
		return "", ErrInvalid
	}
	stamp := s.Now().Unix()
	h, _ := json.Marshal(header{Typ: "JWT", Alg: alg})
	p, _ := json.Marshal(claims{Iss: s.Config.Issuer, Sub: sub, Aud: s.Config.Audience, Iat: stamp, Exp: stamp + 900})
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sig, err := s.Keys[alg].Sign([]byte(input))
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ObjectFields rejects duplicate names and non-object JSON before decoding values.
func ObjectFields(b []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, errors.New("expected object")
	}
	m := make(map[string]json.RawMessage)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := t.(string)
		if !ok {
			return nil, errors.New("invalid field")
		}
		if _, exists := m[name]; exists {
			return nil, errors.New("duplicate field")
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		m[name] = raw
	}
	if t, err = d.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("invalid object")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return m, nil
}

func exact(m map[string]json.RawMessage, names ...string) bool {
	if len(m) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := m[name]; !ok {
			return false
		}
	}
	return true
}

func stringField(m map[string]json.RawMessage, name string) (string, error) {
	var s string
	b := m[name]
	if len(b) == 0 || b[0] != '"' || json.Unmarshal(b, &s) != nil {
		return "", ErrInvalid
	}
	return s, nil
}

func intField(m map[string]json.RawMessage, name string) (int64, error) {
	b := string(m[name])
	if b == "" || strings.ContainsAny(b, ".eE+\" ") {
		return 0, ErrInvalid
	}
	n, err := strconv.ParseInt(b, 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return n, nil
}

func ParseIssueBody(b []byte, c profile.Config) (string, error) {
	m, err := ObjectFields(b)
	if err != nil || !exact(m, "sub") {
		return "", ErrInvalid
	}
	sub, err := stringField(m, "sub")
	if err != nil || !c.ValidSubject(sub) {
		return "", ErrInvalid
	}
	return sub, nil
}

func (s Service) Verify(alg, compact string) error {
	if !profile.Supported(alg) || len(compact) > 16000 {
		return ErrInvalid
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return ErrInvalid
	}
	for _, part := range parts {
		if part == "" || strings.Contains(part, "=") {
			return ErrInvalid
		}
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrInvalid
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrInvalid
	}
	if base64.RawURLEncoding.EncodeToString(hb) != parts[0] || base64.RawURLEncoding.EncodeToString(pb) != parts[1] || base64.RawURLEncoding.EncodeToString(sig) != parts[2] {
		return ErrInvalid
	}
	h, err := ObjectFields(hb)
	if err != nil || !exact(h, "typ", "alg") {
		return ErrInvalid
	}
	typ, err := stringField(h, "typ")
	if err != nil || typ != "JWT" {
		return ErrInvalid
	}
	headerAlg, err := stringField(h, "alg")
	if err != nil || headerAlg != alg {
		return ErrInvalid
	}
	p, err := ObjectFields(pb)
	if err != nil || !exact(p, "iss", "sub", "aud", "iat", "exp") {
		return ErrInvalid
	}
	input := []byte(parts[0] + "." + parts[1])
	if err := s.Keys[alg].Verify(input, sig); err != nil {
		return ErrInvalid
	}
	iss, e1 := stringField(p, "iss")
	sub, e2 := stringField(p, "sub")
	aud, e3 := stringField(p, "aud")
	iat, e4 := intField(p, "iat")
	exp, e5 := intField(p, "exp")
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		return ErrInvalid
	}
	now := s.Now().Unix()
	if iss != s.Config.Issuer || !s.Config.ValidSubject(sub) || aud != s.Config.Audience || iat > now || exp != iat+900 || now >= exp {
		return ErrInvalid
	}
	return nil
}

type Inspection struct {
	PayloadBytes int
	JWTBytes     int
}

func Inspect(compact string) (Inspection, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return Inspection{}, fmt.Errorf("invalid compact token")
	}
	p, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{PayloadBytes: len(p), JWTBytes: len(compact)}, nil
}
