package restore

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

func unsupportedKnowledgeArchive(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range map[string]string{
		"knowledge-bases/legacy/knowledge_base.json": `{"id":"legacy","access_control":null}`,
		"knowledge-bases/new/knowledge_base.json":    `{"id":"new","access_grants":null}`,
		"owui.json": `{"unified_backup":true,"contained_types":["user","knowledge"]}`,
	} {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchivePreflightBeforeAnyClientAccess(t *testing.T) {
	// A nil client deliberately makes any API access fail the test. Every public
	// entry below must reject the archive before user restore, reads or mutations.
	path := unsupportedKnowledgeArchive(t)
	for name, restore := range map[string]func() error{
		"knowledge": func() error { return RestoreKnowledge(nil, path, false) },
		"model":     func() error { return RestoreModel(nil, path, false) },
		"selective": func() error {
			return RestoreSelective(nil, path, &SelectiveRestoreOptions{Knowledge: true, Users: true}, false, nil)
		},
		"unified":      func() error { return restoreFromUnifiedBackup(nil, path, false) },
		"legacy batch": func() error { return restoreFromLegacyBackups(nil, []string{path}, false) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := restore(); err == nil || !strings.Contains(err.Error(), "access_grants") {
				t.Fatalf("expected permission error, got %v", err)
			}
		})
	}
}

func TestArchiveWithoutCollectionsNeedsNoKnowledgeAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	w.Close()
	f.Close()
	var client *openwebui.Client
	if err := preflightKnowledgeArchive(client, path); err != nil {
		t.Fatal(err)
	}
}
