package profile

import (
	"os"
	"path/filepath"
	"testing"
)

const validConfig = `{
  "issuer": "test-issuer",
  "audience": "test-audience",
  "subject_prefix": "vu-",
  "subject_count": 1000,
  "subject_width": 4,
  "key_dir": "keys",
  "listen": ":8080"
}`

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAndSubjectBoundaries(t *testing.T) {
	c, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject(1) != "vu-0001" || c.Subject(1000) != "vu-1000" {
		t.Fatalf("unexpected subjects: %q %q", c.Subject(1), c.Subject(1000))
	}
	for _, subject := range []string{"vu-0001", "vu-0999", "vu-1000"} {
		if !c.ValidSubject(subject) {
			t.Errorf("valid subject rejected: %q", subject)
		}
	}
	for _, subject := range []string{"", "vu-0000", "vu-1001", "vu-001", "vu-00001", "other-0001", "vu-00a1"} {
		if c.ValidSubject(subject) {
			t.Errorf("invalid subject accepted: %q", subject)
		}
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":   `{`,
		"unknown field":    validConfig[:len(validConfig)-1] + `, "unknown": true}`,
		"trailing JSON":    validConfig + `{}`,
		"small population": `{"issuer":"i","audience":"a","subject_prefix":"vu-","subject_count":999,"subject_width":4,"key_dir":"keys","listen":":8080"}`,
		"narrow subject":   `{"issuer":"i","audience":"a","subject_prefix":"vu-","subject_count":1000,"subject_width":3,"key_dir":"keys","listen":":8080"}`,
		"unsafe prefix":    `{"issuer":"i","audience":"a","subject_prefix":"vu-\\n","subject_count":1000,"subject_width":4,"key_dir":"keys","listen":":8080"}`,
		"missing issuer":   `{"audience":"a","subject_prefix":"vu-","subject_count":1000,"subject_width":4,"key_dir":"keys","listen":":8080"}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, contents)); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing configuration was accepted")
	}
}

func TestRepositoryConfiguration(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject(1) != "vu-0001" || c.Subject(1000) != "vu-1000" {
		t.Fatalf("repository subject range is invalid: %q through %q", c.Subject(1), c.Subject(1000))
	}
	for _, alg := range Algorithms {
		if !Supported(alg) {
			t.Errorf("configured algorithm is unsupported: %s", alg)
		}
	}
}
