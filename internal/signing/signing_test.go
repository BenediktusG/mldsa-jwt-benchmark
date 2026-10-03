package signing

import (
	"os"
	"path/filepath"
	"testing"

	"mldsa-jwt-benchmark/internal/profile"
)

func TestGeneratedKeysSignAndVerifyAllAlgorithms(t *testing.T) {
	c := profile.Config{KeyDir: filepath.Join(t.TempDir(), "keys")}
	if err := GenerateFiles(c); err != nil {
		t.Fatal(err)
	}
	if err := GenerateFiles(c); err == nil {
		t.Fatal("GenerateFiles overwrote existing research keys")
	}
	for _, alg := range profile.Algorithms {
		info, err := os.Stat(c.KeyPath(alg))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s permissions = %04o, want 0600", alg, info.Mode().Perm())
		}
	}

	keys, err := LoadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("research signing round-trip")
	for _, alg := range profile.Algorithms {
		t.Run(alg, func(t *testing.T) {
			signature, err := keys[alg].Sign(message)
			if err != nil {
				t.Fatal(err)
			}
			if err := keys[alg].Verify(message, signature); err != nil {
				t.Fatalf("valid signature rejected: %v", err)
			}
			if err := keys[alg].Verify([]byte("modified message"), signature); err == nil {
				t.Fatal("signature accepted for a modified message")
			}
			modified := append([]byte(nil), signature...)
			modified[len(modified)/2] ^= 1
			if err := keys[alg].Verify(message, modified); err == nil {
				t.Fatal("modified signature was accepted")
			}
		})
	}
}

func TestLoadAllRejectsExcessivePermissions(t *testing.T) {
	c := profile.Config{KeyDir: filepath.Join(t.TempDir(), "keys")}
	if err := GenerateFiles(c); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(c.KeyPath("ES256"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAll(c); err == nil {
		t.Fatal("world-readable private key was accepted")
	}
}
