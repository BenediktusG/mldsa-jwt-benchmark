package experiment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func FileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func SourceHash(root string) (string, error) {
	paths := []string{"go.mod", "Dockerfile"}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, item fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !item.IsDir() && strings.HasSuffix(path, ".go") {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				paths = append(paths, filepath.ToSlash(relative))
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, relative := range paths {
		b, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return "", fmt.Errorf("hash %s: %w", relative, err)
		}
		h.Write([]byte(relative))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
