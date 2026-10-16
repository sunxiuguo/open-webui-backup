// Package filearchive preserves original uploads separately from indexed text.
// Version 1 is a file-only capability: metadata is retained as evidence; ownership,
// ACLs, timestamps and knowledge associations are not recreated.
package filearchive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

type Manifest struct {
	Version        int    `json:"version"`
	SourceID       string `json:"source_id"`
	Filename       string `json:"filename"`
	OriginalSHA256 string `json:"original_sha256"`
	IndexedState   string `json:"indexed_state"`
	IndexedSHA256  string `json:"indexed_sha256"`
	MetadataSHA256 string `json:"metadata_sha256"`
}
type Bundle struct {
	Manifest           Manifest
	Metadata, Original []byte
	Indexed            string
}
type RestoreResult struct {
	SourceID string `json:"source_id"`
	NewID    string `json:"new_id"`
	Verified bool   `json:"verified"`
}

// PartialRestoreError reports the newly created ID for inspection; no automatic
// delete or rollback is attempted after a failed update or readback.
type PartialRestoreError struct {
	Result RestoreResult
	Err    error
}

func (e *PartialRestoreError) Error() string {
	return fmt.Sprintf("file %s created as %s but not verified; inspect before retry: %v", e.Result.SourceID, e.Result.NewID, e.Err)
}
func (e *PartialRestoreError) Unwrap() error { return e.Err }
func digest(b []byte) string                 { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00")
}
func metadataFields(raw []byte) (id, filename, state, text string, err error) {
	var f struct {
		ID       string          `json:"id"`
		Filename string          `json:"filename"`
		Data     json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		return
	}
	id, filename = f.ID, f.Filename
	if !safeID(id) || filename == "" || strings.ContainsAny(filename, "/\\\x00\r\n") {
		err = fmt.Errorf("unsafe or missing file identity")
		return
	}
	state = "missing"
	if len(f.Data) == 0 {
		return
	}
	if bytes.Equal(bytes.TrimSpace(f.Data), []byte("null")) {
		state = "null-data"
		return
	}
	var d map[string]json.RawMessage
	if err = json.Unmarshal(f.Data, &d); err != nil {
		return
	}
	c, ok := d["content"]
	if !ok {
		return
	}
	if bytes.Equal(bytes.TrimSpace(c), []byte("null")) {
		state = "null"
		return
	}
	if err = json.Unmarshal(c, &text); err != nil {
		return
	}
	state = "present"
	return
}
func Capture(c *openwebui.Client, id string) (*Bundle, error) {
	if !safeID(id) {
		return nil, fmt.Errorf("unsafe file ID")
	}
	raw, err := c.ReadFileEndpoint("/api/v1/files/" + url.PathEscape(id) + "?content=true")
	if err != nil {
		return nil, err
	}
	got, name, state, text, err := metadataFields(raw)
	if err != nil {
		return nil, err
	}
	if got != id {
		return nil, fmt.Errorf("metadata identity mismatch")
	}
	original, err := c.OriginalFile(id)
	if err != nil {
		return nil, fmt.Errorf("original upload unavailable: %w", err)
	}
	if state == "present" {
		readback, e := c.IndexedFile(id)
		if e != nil {
			return nil, e
		}
		if readback != text {
			return nil, fmt.Errorf("indexed text changed during capture")
		}
	}
	return &Bundle{Manifest: Manifest{1, id, name, digest(original), state, digest([]byte(text)), digest(raw)}, Metadata: raw, Original: original, Indexed: text}, nil
}
func (b *Bundle) Validate() error {
	if b.Manifest.Version != 1 {
		return fmt.Errorf("unsupported file representation version")
	}
	id, name, state, text, err := metadataFields(b.Metadata)
	if err != nil {
		return err
	}
	if id != b.Manifest.SourceID || name != b.Manifest.Filename || state != b.Manifest.IndexedState || text != b.Indexed {
		return fmt.Errorf("file metadata and manifest disagree")
	}
	if digest(b.Original) != b.Manifest.OriginalSHA256 || digest([]byte(b.Indexed)) != b.Manifest.IndexedSHA256 || digest(b.Metadata) != b.Manifest.MetadataSHA256 {
		return fmt.Errorf("file representation checksum mismatch")
	}
	return nil
}
func Write(w *zip.Writer, prefix string, b *Bundle) error {
	if err := b.Validate(); err != nil {
		return err
	}
	manifest, err := json.MarshalIndent(b.Manifest, "", "  ")
	if err != nil {
		return err
	}
	for _, e := range []struct {
		name string
		data []byte
	}{{"representations.json", manifest}, {"file.json", b.Metadata}, {"original.bin", b.Original}, {"indexed.txt", []byte(b.Indexed)}} {
		f, err := w.Create(prefix + e.name)
		if err != nil {
			return err
		}
		if _, err = f.Write(e.data); err != nil {
			return err
		}
	}
	return nil
}

// ValidateArchivePaths rejects ambiguous or unsafe ZIP structure before any restore action.
func ValidateArchivePaths(r *zip.Reader) error {
	seen := map[string]bool{}
	for _, f := range r.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00") || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe archive path %q", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate archive path %q", f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

func Read(r *zip.Reader, prefix string) (*Bundle, error) {
	if err := ValidateArchivePaths(r); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"representations.json": true, "file.json": true, "original.bin": true, "indexed.txt": true}
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, prefix) && !f.FileInfo().IsDir() {
			relative := strings.TrimPrefix(f.Name, prefix)
			if !allowed[relative] && !(prefix == "" && relative == "owui.json") {
				return nil, fmt.Errorf("unexpected file representation entry %q", f.Name)
			}
		}
	}

	read := func(name string) ([]byte, error) {
		var found *zip.File
		for _, f := range r.File {
			if f.Name == prefix+name {
				if found != nil {
					return nil, fmt.Errorf("duplicate archive entry %s", name)
				}
				found = f
			}
		}
		if found == nil {
			return nil, fmt.Errorf("missing %s; legacy archives cannot reconstruct original uploads", name)
		}
		rc, err := found.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	m, err := read("representations.json")
	if err != nil {
		return nil, err
	}
	b := &Bundle{}
	if err = json.Unmarshal(m, &b.Manifest); err != nil {
		return nil, err
	}
	if b.Metadata, err = read("file.json"); err != nil {
		return nil, err
	}
	if b.Original, err = read("original.bin"); err != nil {
		return nil, err
	}
	indexed, err := read("indexed.txt")
	if err != nil {
		return nil, err
	}
	b.Indexed = string(indexed)
	if prefix != "" && prefix != "files/"+b.Manifest.SourceID+"/" {
		return nil, fmt.Errorf("file directory and source ID mismatch")
	}
	if err = b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}
func Restore(c *openwebui.Client, b *Bundle) (RestoreResult, error) {
	return RestoreWithProgress(c, b, nil)
}

// RestoreWithProgress persists attempt/returned identity/verification boundaries.
// A callback failure stops before the next mutating request.
func RestoreWithProgress(c *openwebui.Client, b *Bundle, progress func(RestoreResult, string) error) (RestoreResult, error) {
	result := RestoreResult{SourceID: b.Manifest.SourceID}
	if err := b.Validate(); err != nil {
		return result, err
	}
	if b.Manifest.IndexedState != "present" {
		return result, fmt.Errorf("indexed state %q cannot be recreated with the supported update API", b.Manifest.IndexedState)
	}
	if b.Indexed == "" {
		return result, fmt.Errorf("empty indexed text cannot be faithfully restored by the pinned processing API")
	}
	existing, err := c.ListRepresentationFiles()
	if err != nil {
		return result, err
	}
	if progress != nil {
		if err := progress(result, "uploading"); err != nil {
			return result, err
		}
	}
	uploaded, err := c.UploadFile(b.Manifest.Filename, b.Original)
	if err != nil {
		return result, fmt.Errorf("upload outcome unverified; the server may have created a file, inspect before retry: %w", err)
	}
	result.NewID = uploaded.ID
	fail := func(err error) (RestoreResult, error) { return result, &PartialRestoreError{result, err} }
	if progress != nil {
		if err := progress(result, "uploaded"); err != nil {
			return fail(err)
		}
	}
	if !safeID(result.NewID) {
		return fail(fmt.Errorf("upload returned invalid ID"))
	}
	for _, f := range existing {
		if f.ID == result.NewID {
			return fail(fmt.Errorf("upload did not create a new ID; refusing indexed update"))
		}
	}

	if err = c.UpdateIndexedFile(result.NewID, b.Indexed); err != nil {
		return fail(err)
	}
	original, err := c.OriginalFile(result.NewID)
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(original, b.Original) {
		return fail(fmt.Errorf("original upload readback mismatch"))
	}
	indexed, err := c.IndexedFile(result.NewID)
	if err != nil {
		return fail(err)
	}
	if indexed != b.Indexed {
		return fail(fmt.Errorf("indexed text readback mismatch"))
	}
	result.Verified = true
	if progress != nil {
		if err := progress(result, "verified"); err != nil {
			return fail(err)
		}
	}
	return result, nil
}
