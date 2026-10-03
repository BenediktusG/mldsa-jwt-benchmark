package experiment

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSourceHashOnlyTracksImageSourceInputs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "Dockerfile", "FROM scratch\n")
	writeTestFile(t, root, "cmd/server/main.go", "package main\n")
	writeTestFile(t, root, "internal/example/example.go", "package example\n")
	writeTestFile(t, root, "compose.yaml", "services: {}\n")
	writeTestFile(t, root, "load/scenario.js", "export default function () {}\n")

	original, err := SourceHash(root)
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, root, "compose.yaml", "services:\n  changed: {}\n")
	writeTestFile(t, root, "load/scenario.js", "export default function () { return 1 }\n")
	withoutRuntimeFiles, err := SourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if withoutRuntimeFiles != original {
		t.Fatal("runtime-only files changed the source hash")
	}

	writeTestFile(t, root, "internal/example/example.go", "package example\nconst Changed = true\n")
	withSourceChange, err := SourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if withSourceChange == original {
		t.Fatal("Go source change did not change the source hash")
	}
}
