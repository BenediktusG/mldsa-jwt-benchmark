package server_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/server"
	"mldsa-jwt-benchmark/internal/signing"
	"mldsa-jwt-benchmark/internal/token"
)

func testService(t *testing.T) token.Service {
	t.Helper()
	c := profile.Config{Issuer: "test-issuer", Audience: "test-audience", SubjectPrefix: "vu-", SubjectCount: 1000, SubjectWidth: 4, KeyDir: filepath.Join(t.TempDir(), "keys"), Listen: ":0"}
	if err := signing.GenerateFiles(c); err != nil {
		t.Fatal(err)
	}
	keys, err := signing.LoadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	return token.Service{Config: c, Keys: keys, Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
}

func request(h http.Handler, method, path, contentType, body, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func signed(t *testing.T, s token.Service, alg, h, p string) string {
	t.Helper()
	input := base64.RawURLEncoding.EncodeToString([]byte(h)) + "." + base64.RawURLEncoding.EncodeToString([]byte(p))
	sig, err := s.Keys[alg].Sign([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestFunctionalProfile(t *testing.T) {
	s := testService(t)
	h := server.Server{Service: s}
	for _, alg := range profile.Algorithms {
		t.Run(alg, func(t *testing.T) {
			w := request(h, "POST", "/token?alg="+alg, "application/json", `{"sub":"vu-0001"}`, "")
			if w.Code != 200 {
				t.Fatalf("issue %d: %s", w.Code, w.Body.String())
			}
			var response struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if err := s.Verify(alg, response.Token); err != nil {
				t.Fatal(err)
			}
			v := request(h, "GET", "/protected?alg="+alg, "", "", "Bearer "+response.Token)
			if v.Code != 200 || v.Body.String() != `{"status":"success"}` {
				t.Fatalf("verify %d: %s", v.Code, v.Body.String())
			}
			parts := strings.Split(response.Token, ".")
			parts[2] = strings.Repeat("A", len(parts[2]))
			if w := request(h, "GET", "/protected?alg="+alg, "", "", "Bearer "+strings.Join(parts, ".")); w.Code != 401 {
				t.Fatalf("tampered signature: %d", w.Code)
			}
			other := "ES256"
			if alg == other {
				other = "ES384"
			}
			if w := request(h, "GET", "/protected?alg="+other, "", "", "Bearer "+response.Token); w.Code != 401 {
				t.Fatalf("wrong algorithm: %d", w.Code)
			}
		})
	}
	for name, tc := range map[string]struct {
		method, path, contentType, body, auth string
		status                                int
		code                                  string
	}{
		"duplicate sub":     {"POST", "/token?alg=ES256", "application/json", `{"sub":"vu-0001","sub":"vu-0002"}`, "", 400, "invalid_request"},
		"extra field":       {"POST", "/token?alg=ES256", "application/json", `{"sub":"vu-0001","iat":1}`, "", 400, "invalid_request"},
		"null sub":          {"POST", "/token?alg=ES256", "application/json", `{"sub":null}`, "", 400, "invalid_request"},
		"unknown sub":       {"POST", "/token?alg=ES256", "application/json", `{"sub":"vu-1001"}`, "", 400, "invalid_request"},
		"extra query":       {"POST", "/token?alg=ES256&x=1", "application/json", `{"sub":"vu-0001"}`, "", 400, "invalid_request"},
		"duplicate query":   {"POST", "/token?alg=ES256&alg=ES256", "application/json", `{"sub":"vu-0001"}`, "", 400, "invalid_request"},
		"media type":        {"POST", "/token?alg=ES256", "text/plain", `{"sub":"vu-0001"}`, "", 415, "unsupported_media_type"},
		"method":            {"GET", "/token?alg=ES256", "", "", "", 400, "invalid_request"},
		"missing bearer":    {"GET", "/protected?alg=ES256", "", "", "", 401, "invalid_token"},
		"body on protected": {"GET", "/protected?alg=ES256", "", "x", "Bearer bad", 400, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			w := request(h, tc.method, tc.path, tc.contentType, tc.body, tc.auth)
			if w.Code != tc.status || w.Body.String() != fmt.Sprintf(`{"error":"%s"}`, tc.code) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	good := `{"iss":"test-issuer","sub":"vu-0001","aud":"test-audience","iat":1800000000,"exp":1800000900}`
	for name, payload := range map[string]string{
		"duplicate claim": `{"iss":"test-issuer","iss":"test-issuer","sub":"vu-0001","aud":"test-audience","iat":1800000000,"exp":1800000900}`,
		"missing claim":   `{"iss":"test-issuer","sub":"vu-0001","aud":"test-audience","iat":1800000000}`,
		"wrong issuer":    strings.Replace(good, "test-issuer", "other", 1),
		"wrong subject":   strings.Replace(good, "vu-0001", "vu-9999", 1),
		"wrong audience":  strings.Replace(good, "test-audience", "other", 1),
		"future iat":      strings.Replace(good, "1800000000", "1800000001", 1),
		"wrong expiry":    strings.Replace(good, "1800000900", "1800000901", 1),
		"expired":         `{"iss":"test-issuer","sub":"vu-0001","aud":"test-audience","iat":1799999000,"exp":1799999900}`,
		"wrong type":      strings.Replace(good, `"iat":1800000000`, `"iat":"1800000000"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			jwt := signed(t, s, "ES256", `{"typ":"JWT","alg":"ES256"}`, payload)
			if err := s.Verify("ES256", jwt); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
	for name, header := range map[string]string{
		"duplicate header":   `{"typ":"JWT","alg":"ES256","alg":"ES256"}`,
		"none":               `{"typ":"JWT","alg":"none"}`,
		"critical extension": `{"typ":"JWT","alg":"ES256","crit":["x"]}`,
		"malformed JSON":     `{"typ":"JWT","alg":"ES256"`,
	} {
		t.Run(name, func(t *testing.T) {
			jwt := signed(t, s, "ES256", header, good)
			if err := s.Verify("ES256", jwt); err == nil {
				t.Fatal("invalid header accepted")
			}
		})
	}
	if err := s.Verify("ES256", "!!!.abc.def"); err == nil {
		t.Fatal("invalid Base64url accepted")
	}
}

func TestConcurrentRequests(t *testing.T) {
	s := testService(t)
	h := server.Server{Service: s}
	var wg sync.WaitGroup
	for _, alg := range profile.Algorithms {
		for i := 0; i < 8; i++ {
			wg.Go(func() {
				w := request(h, "POST", "/token?alg="+alg, "application/json", `{"sub":"vu-0001"}`, "")
				if w.Code != 200 {
					t.Errorf("issue %s: %d", alg, w.Code)
					return
				}
				var v struct {
					Token string `json:"token"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
					t.Error(err)
					return
				}
				if err := s.Verify(alg, v.Token); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
}
