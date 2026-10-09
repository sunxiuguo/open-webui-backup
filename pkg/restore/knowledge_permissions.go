package restore

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

// Validate every collection before any write in this archive. Checking inside
// the restore loop would allow map iteration order to produce partial restores.
func preflightKnowledgeArchive(client *openwebui.Client, path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("knowledge preflight: %w", err)
	}
	defer r.Close()
	found := false
	for _, f := range r.File {
		if f.Name != "knowledge_base.json" && !(strings.HasPrefix(f.Name, "knowledge-bases/") && strings.HasSuffix(f.Name, "/knowledge_base.json")) {
			continue
		}
		found = true
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("knowledge preflight %s: %w", f.Name, err)
		}
		var kb *openwebui.KnowledgeBase
		decoder := json.NewDecoder(rc)
		err = decoder.Decode(&kb)
		if err == nil {
			var trailing interface{}
			if end := decoder.Decode(&trailing); end != io.EOF {
				err = fmt.Errorf("expected one JSON object")
			}
		}
		rc.Close()
		if err != nil {
			return fmt.Errorf("knowledge preflight %s: %w", f.Name, err)
		}
		if kb == nil {
			return fmt.Errorf("knowledge preflight %s: expected a JSON object", f.Name)
		}
		if err := kb.CheckRestorePermissions(); err != nil {
			return err
		}
	}
	if found {
		return client.CheckKnowledgePermissions()
	}
	return nil
}
