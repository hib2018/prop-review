package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillCopiesMatch(t *testing.T) {
	names, err := fs.Glob(Files, "*/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no bundled skills found")
	}
	for _, name := range names {
		want, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{".agents", ".pi"} {
			t.Run(dir+"/"+name, func(t *testing.T) {
				path := filepath.Join("..", dir, "skills", name)
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s differs from bundled skills/%s", path, name)
				}
			})
		}
	}
}
