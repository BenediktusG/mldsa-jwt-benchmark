package experiment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComposeImageResolvesImageID(t *testing.T) {
	binDir := t.TempDir()
	docker := filepath.Join(binDir, "docker")
	script := `#!/bin/sh
if [ "$1 $2 $3" = "compose config --images" ]; then
	printf '%s\n' 'example-server'
	exit 0
fi
if [ "$1 $2 $3" = "image inspect --format" ]; then
	printf '%s\n' 'sha256:abc123'
	exit 0
fi
exit 1
`
	if err := os.WriteFile(docker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	name, id, err := composeImage(t.TempDir(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if name != "example-server" {
		t.Fatalf("image name = %q, want example-server", name)
	}
	if id != "sha256:abc123" {
		t.Fatalf("image ID = %q, want sha256:abc123", id)
	}
}
