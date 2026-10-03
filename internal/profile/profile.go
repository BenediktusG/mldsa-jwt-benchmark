package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var Algorithms = []string{"ES256", "ES384", "ES512", "ML-DSA-44", "ML-DSA-65", "ML-DSA-87"}

type Config struct {
	Issuer        string `json:"issuer"`
	Audience      string `json:"audience"`
	SubjectPrefix string `json:"subject_prefix"`
	SubjectCount  int    `json:"subject_count"`
	SubjectWidth  int    `json:"subject_width"`
	KeyDir        string `json:"key_dir"`
	Listen        string `json:"listen"`
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var c Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("trailing configuration data")
	}
	if c.Issuer == "" || c.Audience == "" || c.SubjectPrefix == "" || c.SubjectCount < 1000 || c.SubjectWidth < len(strconv.Itoa(c.SubjectCount)) || c.KeyDir == "" || c.Listen == "" {
		return c, fmt.Errorf("invalid configuration")
	}
	if strings.ContainsAny(c.SubjectPrefix, "\"\\\n\r") {
		return c, fmt.Errorf("invalid subject prefix")
	}
	return c, nil
}

func (c Config) Subject(i int) string {
	return fmt.Sprintf("%s%0*d", c.SubjectPrefix, c.SubjectWidth, i)
}

func (c Config) ValidSubject(s string) bool {
	if !strings.HasPrefix(s, c.SubjectPrefix) || len(s) != len(c.SubjectPrefix)+c.SubjectWidth {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, c.SubjectPrefix))
	return err == nil && n >= 1 && n <= c.SubjectCount && c.Subject(n) == s
}

func (c Config) KeyPath(alg string) string { return filepath.Join(c.KeyDir, alg+".json") }

func Supported(alg string) bool {
	for _, a := range Algorithms {
		if a == alg {
			return true
		}
	}
	return false
}
