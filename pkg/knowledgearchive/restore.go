package knowledgearchive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/vosiander/open-webui-backup/pkg/filearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

type Attempt struct {
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id,omitempty"`
	State    string `json:"state"`
}
type Report struct {
	Version     int               `json:"version"`
	SourceOwner string            `json:"source_owner"`
	TargetOwner string            `json:"target_owner,omitempty"`
	Status      string            `json:"status"`
	Files       map[string]string `json:"file_id_map"`
	Knowledge   map[string]string `json:"knowledge_id_map"`
	Attempts    []Attempt         `json:"attempts"`
	Error       string            `json:"error,omitempty"`
}
type RestoreError struct {
	ReportPath string
	Report     Report
	Err        error
}

func (e *RestoreError) Error() string {
	return fmt.Sprintf("knowledge restore incomplete; inspect %s before retry: %v", e.ReportPath, e.Err)
}
func (e *RestoreError) Unwrap() error { return e.Err }

// persist atomically replaces only the fresh report created for this invocation.
func persist(path string, r *Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".kb-report-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func targetOwned(c *openwebui.Client) (map[string]Knowledge, string, error) {
	raw, err := ListRaw(c, OwnedEndpoint, false)
	if err != nil {
		return nil, "", err
	}
	items := map[string]Knowledge{}
	owner := ""
	for _, v := range raw {
		var k Knowledge
		if err = json.Unmarshal(v, &k); err != nil {
			return nil, "", err
		}
		if !validID(k.UserID) {
			return nil, "", fmt.Errorf("invalid owned knowledge response")
		}
		if owner != "" && owner != k.UserID {
			return nil, "", fmt.Errorf("inconsistent authenticated owner")
		}
		owner = k.UserID
		items[k.ID] = k
	}
	return items, owner, nil
}
func verifyKnowledge(c *openwebui.Client, id, owner, name, description string) error {
	raw, err := c.ReadFileEndpoint("/api/v1/knowledge/" + url.PathEscape(id))
	if err != nil {
		return err
	}
	k, err := decodeKnowledge(raw, owner)
	if err != nil {
		return err
	}
	if k.ID != id || k.Name != name || k.Description != description {
		return fmt.Errorf("knowledge readback mismatch")
	}
	owned, gotOwner, err := targetOwned(c)
	if err != nil {
		return err
	}
	if gotOwner != owner {
		return fmt.Errorf("authenticated owner changed")
	}
	entry, ok := owned[id]
	if !ok || entry.UserID != owner {
		return fmt.Errorf("created knowledge missing from authenticated owned scope")
	}
	return nil
}

// Restore returns a durable report location even for partial outcomes. No deletion,
// overwrite, automatic retry, or resume is attempted. The caller chooses reportDir.
func Restore(c *openwebui.Client, b *Bundle, reportDir string) (reportPath string, report Report, err error) {
	report = Report{Version: 1, SourceOwner: b.Manifest.SourceOwner, Status: "preflight", Files: map[string]string{}, Knowledge: map[string]string{}, Attempts: []Attempt{}}
	if err = b.Validate(); err != nil {
		return
	}
	for _, id := range b.Manifest.FileIDs {
		if b.Files[id].Indexed == "" {
			err = fmt.Errorf("empty indexed text for %s cannot be faithfully restored by pinned processing API", id)
			return
		}
	}
	if len(b.Manifest.Knowledge) == 0 {
		report.Status = "complete-empty"
		return
	}
	if e := RequireVersion(c); e != nil {
		err = e
		return
	}
	// Read-only capabilities and complete archive validation precede all mutations.
	all, e := ListRaw(c, "/api/v1/knowledge/", false)
	if e != nil {
		err = e
		return
	}
	existing := map[string]bool{}
	for _, raw := range all {
		var k Knowledge
		json.Unmarshal(raw, &k)
		existing[k.ID] = true
	}
	_, owner, e := targetOwned(c)
	if e != nil {
		err = e
		return
	}
	report.TargetOwner = owner
	if _, e = c.ListRepresentationFiles(); e != nil {
		err = e
		return
	}
	f, e := os.CreateTemp(reportDir, "knowledge-restore-*.json")
	if e != nil {
		err = e
		return
	}
	reportPath = f.Name()
	if e = f.Close(); e != nil {
		err = e
		return
	}
	if e = persist(reportPath, &report); e != nil {
		err = e
		return
	}
	fail := func(e error) (string, Report, error) {
		report.Status = "incomplete"
		report.Error = e.Error()
		if saveErr := persist(reportPath, &report); saveErr != nil {
			e = fmt.Errorf("%w; report persistence also failed: %v; in-memory IDs: files=%v knowledge=%v", e, saveErr, report.Files, report.Knowledge)
		}
		return reportPath, report, &RestoreError{reportPath, report, e}
	}
	record := func(kind, source, target, state string) error {
		report.Attempts = append(report.Attempts, Attempt{kind, source, target, state})
		report.Status = "in-progress"
		return persist(reportPath, &report)
	}
	// Create private KB shells first. Their returned ownership is verified against
	// the authenticated created-query contract, without identity probe objects.
	for _, rec := range b.Manifest.Knowledge {
		k, _ := decodeKnowledge(b.Metadata[rec.SourceID], b.Manifest.SourceOwner)
		if e = record("knowledge", rec.SourceID, "", "creating-unknown-outcome"); e != nil {
			return fail(e)
		}
		raw, e := c.PostJSON("/api/v1/knowledge/create", map[string]any{"name": k.Name, "description": k.Description, "access_grants": []any{}})
		if e != nil {
			return fail(fmt.Errorf("knowledge creation outcome unknown: %w", e))
		}
		var created Knowledge
		if e = json.Unmarshal(raw, &created); e != nil {
			return fail(fmt.Errorf("knowledge creation response invalid; outcome unknown: %w", e))
		}
		if !validID(created.ID) {
			return fail(fmt.Errorf("knowledge creation returned no valid ID; outcome unknown"))
		}
		report.Knowledge[rec.SourceID] = created.ID
		if e = record("knowledge", rec.SourceID, created.ID, "created-unverified"); e != nil {
			return fail(e)
		}
		if existing[created.ID] {
			return fail(fmt.Errorf("knowledge create reused an existing ID"))
		}
		existing[created.ID] = true
		if !validID(created.UserID) || (owner != "" && created.UserID != owner) {
			return fail(fmt.Errorf("knowledge creation owner mismatch"))
		}
		owner = created.UserID
		report.TargetOwner = owner
		if _, e = decodeKnowledge(raw, owner); e != nil {
			return fail(e)
		}
		if e = verifyKnowledge(c, created.ID, owner, k.Name, k.Description); e != nil {
			return fail(e)
		}
		ids, e := AssociationIDs(c, created.ID, owner)
		if e != nil {
			return fail(e)
		}
		if len(ids) != 0 {
			return fail(fmt.Errorf("new knowledge already contains files"))
		}
		if e = record("knowledge", rec.SourceID, created.ID, "verified-private-empty"); e != nil {
			return fail(e)
		}
	}
	for _, id := range b.Manifest.FileIDs {
		_, e = filearchive.RestoreWithProgress(c, b.Files[id], func(result filearchive.RestoreResult, state string) error {
			if result.NewID != "" {
				report.Files[id] = result.NewID
			}
			return record("file", id, result.NewID, state)
		})
		if e != nil {
			return fail(e)
		}
		// Verify uploaded file identity/owner as well as A/B representation readback.
		raw, e := c.ReadFileEndpoint("/api/v1/files/" + url.PathEscape(report.Files[id]) + "?content=true")
		if e != nil {
			return fail(e)
		}
		var meta struct {
			ID       string `json:"id"`
			UserID   string `json:"user_id"`
			Filename string `json:"filename"`
		}
		if e = json.Unmarshal(raw, &meta); e != nil {
			return fail(e)
		}
		if meta.ID != report.Files[id] || meta.UserID != owner || meta.Filename != b.Files[id].Manifest.Filename {
			return fail(fmt.Errorf("restored file owner or filename mismatch"))
		}
	}
	// Only verified file IDs are linked. Verify each acknowledgement by membership
	// and verify the final exact set, not merely HTTP 200 or a returned payload.
	for _, rec := range b.Manifest.Knowledge {
		id := report.Knowledge[rec.SourceID]
		expected := []string{}
		for _, sourceFile := range rec.FileIDs {
			targetFile := report.Files[sourceFile]
			if e = record("link", rec.SourceID+"/"+sourceFile, targetFile, "linking-unknown-outcome"); e != nil {
				return fail(e)
			}
			if _, e = c.PostJSON("/api/v1/knowledge/"+url.PathEscape(id)+"/file/add", map[string]any{"file_id": targetFile, "directory_id": nil}); e != nil {
				return fail(e)
			}
			expected = append(expected, targetFile)
			sort.Strings(expected)
			got, e := AssociationIDs(c, id, owner)
			if e != nil {
				return fail(e)
			}
			if !equalIDs(got, expected) {
				return fail(fmt.Errorf("knowledge link acknowledgement did not produce expected membership"))
			}
			if e = record("link", rec.SourceID+"/"+sourceFile, targetFile, "verified"); e != nil {
				return fail(e)
			}
		}
		k, _ := decodeKnowledge(b.Metadata[rec.SourceID], b.Manifest.SourceOwner)
		if e = verifyKnowledge(c, id, owner, k.Name, k.Description); e != nil {
			return fail(e)
		}
		got, e := AssociationIDs(c, id, owner)
		if e != nil {
			return fail(e)
		}
		if !equalIDs(got, expected) {
			return fail(fmt.Errorf("final association mismatch"))
		}
	}

	// Linking can trigger server-side processing. Recheck representations and every
	// private KB graph after all links before publishing a complete result.
	for _, source := range b.Manifest.FileIDs {
		target := report.Files[source]
		original, e := c.OriginalFile(target)
		if e != nil {
			return fail(e)
		}
		indexed, e := c.IndexedFile(target)
		if e != nil {
			return fail(e)
		}
		if !bytes.Equal(original, b.Files[source].Original) || indexed != b.Files[source].Indexed {
			return fail(fmt.Errorf("file representations changed during knowledge linking"))
		}
	}
	for _, rec := range b.Manifest.Knowledge {
		id := report.Knowledge[rec.SourceID]
		k, _ := decodeKnowledge(b.Metadata[rec.SourceID], b.Manifest.SourceOwner)
		if e = verifyKnowledge(c, id, owner, k.Name, k.Description); e != nil {
			return fail(e)
		}
		expected := []string{}
		for _, file := range rec.FileIDs {
			expected = append(expected, report.Files[file])
		}
		sort.Strings(expected)
		got, e := AssociationIDs(c, id, owner)
		if e != nil {
			return fail(e)
		}
		if !equalIDs(got, expected) {
			return fail(fmt.Errorf("final knowledge graph changed"))
		}
	}
	report.Status = "complete"
	if e = persist(reportPath, &report); e != nil {
		return fail(e)
	}
	return reportPath, report, nil
}
