package cmd

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestSkillInstalledCopyMatchesEmbed guards against the drift that happens when
// someone edits an embedded skill asset but forgets to re-run `sr claude
// install`, leaving the checked-in .claude/skills/stackr copy stale. It asserts
// the installed copy matches the embedded source byte-for-byte, in both
// directions (nothing missing, nothing extra).
func TestSkillInstalledCopyMatchesEmbed(t *testing.T) {
	installed := filepath.Join("..", skillDir)

	embedded := map[string][]byte{}
	err := fs.WalkDir(skillAssets, skillAssetRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(skillAssetRoot, p)
		if err != nil {
			return err
		}
		data, err := skillAssets.ReadFile(p)
		if err != nil {
			return err
		}
		embedded[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded assets: %v", err)
	}
	if len(embedded) == 0 {
		t.Fatal("no embedded skill assets found")
	}

	// Every embedded file must exist in the installed copy with identical bytes.
	for rel, want := range embedded {
		got, err := os.ReadFile(filepath.Join(installed, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("installed skill missing %s (run `sr claude install`): %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("installed skill %s is out of date — run `sr claude install`", rel)
		}
	}

	// The installed copy must not contain extra files beyond the embed.
	err = filepath.WalkDir(installed, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(installed, p)
		if err != nil {
			return err
		}
		if _, ok := embedded[filepath.ToSlash(rel)]; !ok {
			t.Errorf("installed skill has stale extra file %s — run `sr claude install`", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking installed skill dir: %v", err)
	}
}
