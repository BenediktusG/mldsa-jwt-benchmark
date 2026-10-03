package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/token"
)

type Server struct{ Service token.Service }

type errorResponse struct {
	Error string `json:"error"`
}

type statusResponse struct {
	Status string `json:"status"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

func reply(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal_error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func bad(w http.ResponseWriter, status int, code string) {
	reply(w, status, errorResponse{Error: code})
}

func algorithm(r *http.Request) (string, bool) {
	q := r.URL.Query()
	if len(q) != 1 || len(q["alg"]) != 1 || !profile.Supported(q.Get("alg")) {
		return "", false
	}
	return q.Get("alg"), true
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		reply(w, http.StatusOK, statusResponse{Status: "ready"})
		return
	}
	if r.URL.Path != "/token" && r.URL.Path != "/protected" {
		bad(w, 400, "invalid_request")
		return
	}
	if (r.URL.Path == "/token" && r.Method != http.MethodPost) || (r.URL.Path == "/protected" && r.Method != http.MethodGet) {
		bad(w, 400, "invalid_request")
		return
	}
	alg, ok := algorithm(r)
	if !ok {
		bad(w, 400, "invalid_request")
		return
	}
	if r.URL.Path == "/token" {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			bad(w, 415, "unsupported_media_type")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
		if err != nil {
			bad(w, 400, "invalid_request")
			return
		}
		sub, err := token.ParseIssueBody(body, s.Service.Config)
		if err != nil {
			bad(w, 400, "invalid_request")
			return
		}
		jwt, err := s.Service.Issue(alg, sub)
		if err != nil {
			bad(w, 500, "internal_error")
			return
		}
		reply(w, http.StatusOK, tokenResponse{Token: jwt})
		return
	}
	if r.ContentLength > 0 {
		bad(w, 400, "invalid_request")
		return
	}
	if r.Body != nil {
		one := make([]byte, 1)
		n, err := r.Body.Read(one)
		if n > 0 || (err != nil && err != io.EOF) {
			bad(w, 400, "invalid_request")
			return
		}
	}
	auth := r.Header.Values("Authorization")
	if len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") || strings.TrimSpace(auth[0]) != auth[0] || strings.ContainsAny(strings.TrimPrefix(auth[0], "Bearer "), " \t\r\n") {
		bad(w, 401, "invalid_token")
		return
	}
	if err := s.Service.Verify(alg, strings.TrimPrefix(auth[0], "Bearer ")); err != nil {
		bad(w, 401, "invalid_token")
		return
	}
	reply(w, http.StatusOK, statusResponse{Status: "success"})
}
