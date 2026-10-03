package token

import (
	"encoding/base64"
	"testing"
)

func TestInspect(t *testing.T) {
	payload := []byte(`{"sub":"vu-0001"}`)
	compact := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	inspection, err := Inspect(compact)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.PayloadBytes != len(payload) || inspection.JWTBytes != len(compact) {
		t.Fatalf("inspection = %+v, want payload bytes %d and JWT bytes %d", inspection, len(payload), len(compact))
	}
	if _, err := Inspect("invalid"); err == nil {
		t.Fatal("invalid compact token was accepted")
	}
}
