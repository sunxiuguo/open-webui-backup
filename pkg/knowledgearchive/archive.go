// Package knowledgearchive implements owned-private, root-only knowledge bundles.
package knowledgearchive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/vosiander/open-webui-backup/pkg/filearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

const SupportedVersion = "0.12.2"

const OwnedEndpoint = "/api/v1/knowledge/search?view_option=created&order_by=created_at&direction=asc"

type Knowledge struct {
	ID          string             `json:"id"`
	UserID      string             `json:"user_id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Grants      *[]json.RawMessage `json:"access_grants"`
	Meta        json.RawMessage    `json:"meta"`
	FileCount   *int               `json:"file_count,omitempty"`
}
type Record struct {
	SourceID       string   `json:"source_id"`
	FileIDs        []string `json:"file_ids"`
	MetadataSHA256 string   `json:"metadata_sha256"`
}
type Manifest struct {
	SourceVersion string   `json:"source_version"`
	Version       int      `json:"version"`
	SourceOwner   string   `json:"source_owner"`
	Knowledge     []Record `json:"knowledge"`
	FileIDs       []string `json:"file_ids"`
}
type Bundle struct {
	Manifest Manifest
	Metadata map[string]json.RawMessage
	Files    map[string]*filearchive.Bundle
}

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00?#")
}
func emptyObject(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil && len(m) == 0
}
func decodeKnowledge(raw []byte, owner string) (Knowledge, error) {
	var k Knowledge
	if err := json.Unmarshal(raw, &k); err != nil {
		return k, err
	}
	if !validID(k.ID) || k.UserID != owner || k.Name == "" || k.Grants == nil || len(*k.Grants) != 0 || !emptyObject(k.Meta) {
		return k, fmt.Errorf("knowledge %q is not explicitly owned-private with supported empty metadata", k.ID)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return k, err
	}
	if _, ok := fields["access_control"]; ok {
		return k, fmt.Errorf("legacy access_control is unsupported")
	}
	if v, ok := fields["data"]; ok && !emptyObject(v) {
		return k, fmt.Errorf("legacy knowledge data is unsupported")
	}
	if _, ok := fields["description"]; !ok {
		return k, fmt.Errorf("knowledge description missing")
	}
	return k, nil
}

// ListRaw reads every page and fails on duplicates, changing totals and gaps.
func ListRaw(c *openwebui.Client, endpoint string, associations bool) ([]json.RawMessage, error) {
	result := []json.RawMessage{}
	seen := map[string]bool{}
	total := -1
	for page := 1; ; page++ {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		raw, err := c.ReadFileEndpoint(endpoint + separator + "page=" + fmt.Sprint(page))
		if err != nil {
			return nil, err
		}
		var p struct {
			Items       *[]json.RawMessage `json:"items"`
			Total       *int               `json:"total"`
			Directories *[]json.RawMessage `json:"directories"`
			Breadcrumbs *[]json.RawMessage `json:"breadcrumbs"`
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Items == nil || p.Total == nil || *p.Total < 0 {
			return nil, fmt.Errorf("invalid paginated response")
		}
		if total >= 0 && total != *p.Total {
			return nil, fmt.Errorf("pagination total changed")
		}
		total = *p.Total
		if associations && (p.Directories == nil || len(*p.Directories) != 0 || p.Breadcrumbs == nil || len(*p.Breadcrumbs) != 0) {
			return nil, fmt.Errorf("directories or unverified directory capability unsupported")
		}
		for _, item := range *p.Items {
			var id struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(item, &id); err != nil {
				return nil, err
			}
			if !validID(id.ID) || seen[id.ID] {
				return nil, fmt.Errorf("invalid or duplicate paginated ID")
			}
			seen[id.ID] = true
			result = append(result, item)
		}
		if len(result) == total {
			return result, nil
		}
		if len(result) > total || len(*p.Items) == 0 {
			return nil, fmt.Errorf("incomplete pagination")
		}
	}
}
func AssociationIDs(c *openwebui.Client, id, owner string) ([]string, error) {
	items, err := ListRaw(c, "/api/v1/knowledge/"+url.PathEscape(id)+"/files", true)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, raw := range items {
		var f struct {
			ID            string  `json:"id"`
			UserID        string  `json:"user_id"`
			DirectoryID   *string `json:"directory_id"`
			DirectoryPath *string `json:"directory_path"`
			HasOriginal   *bool   `json:"has_original"`
		}
		if err = json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		json.Unmarshal(raw, &fields)
		if _, ok := fields["directory_id"]; !ok {
			return nil, fmt.Errorf("directory identity missing")
		}
		if f.UserID != owner || f.DirectoryID != nil || f.DirectoryPath == nil || *f.DirectoryPath != "" || f.HasOriginal == nil || !*f.HasOriginal {
			return nil, fmt.Errorf("file %s is not owned, root-only, original-backed", f.ID)
		}
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	return ids, nil
}
func Capture(c *openwebui.Client) (*Bundle, error) {
	if err := RequireVersion(c); err != nil {
		return nil, err
	}
	listed, err := ListRaw(c, "/api/v1/knowledge/", false)
	if err != nil {
		return nil, err
	}
	owned, err := ListRaw(c, OwnedEndpoint, false)
	if err != nil {
		return nil, err
	}
	if len(owned) != len(listed) {
		return nil, fmt.Errorf("visible knowledge includes nonowned resources; unsupported scope")
	}
	ownedIDs := map[string]bool{}
	owner := ""
	for _, raw := range owned {
		var k Knowledge
		if err = json.Unmarshal(raw, &k); err != nil {
			return nil, err
		}
		if !validID(k.UserID) {
			return nil, fmt.Errorf("owner identity unavailable")
		}
		if owner != "" && owner != k.UserID {
			return nil, fmt.Errorf("created query returned inconsistent owners")
		}
		owner = k.UserID
		ownedIDs[k.ID] = true
	}
	for _, raw := range listed {
		var k Knowledge
		json.Unmarshal(raw, &k)
		if !ownedIDs[k.ID] {
			return nil, fmt.Errorf("visible knowledge not in authenticated created scope")
		}
	}

	b := &Bundle{Manifest: Manifest{Version: 1, SourceVersion: SupportedVersion, SourceOwner: owner, Knowledge: []Record{}, FileIDs: []string{}}, Metadata: map[string]json.RawMessage{}, Files: map[string]*filearchive.Bundle{}}
	for _, raw := range listed {
		k, err := decodeKnowledge(raw, owner)
		if err != nil {
			return nil, err
		}
		if k.FileCount == nil || *k.FileCount < 0 {
			return nil, fmt.Errorf("knowledge file count unavailable")
		}
		detail, err := c.ReadFileEndpoint("/api/v1/knowledge/" + url.PathEscape(k.ID))
		if err != nil {
			return nil, err
		}
		d, err := decodeKnowledge(detail, owner)
		if err != nil {
			return nil, err
		}
		if d.ID != k.ID || d.Name != k.Name || d.Description != k.Description {
			return nil, fmt.Errorf("knowledge changed during capture")
		}
		ids, err := AssociationIDs(c, k.ID, owner)
		if err != nil {
			return nil, err
		}
		if len(ids) != *k.FileCount {
			return nil, fmt.Errorf("knowledge association count disagrees with list")
		}
		b.Metadata[k.ID] = detail
		b.Manifest.Knowledge = append(b.Manifest.Knowledge, Record{k.ID, ids, hash(detail)})
		for _, id := range ids {
			if _, ok := b.Files[id]; ok {
				continue
			}
			file, err := filearchive.Capture(c, id)
			if err != nil {
				return nil, err
			}
			if file.Manifest.IndexedState != "present" {
				return nil, fmt.Errorf("file %s has non-restorable indexed state", id)
			}
			if err = checkFileOwner(file, owner); err != nil {
				return nil, err
			}
			b.Files[id] = file
			b.Manifest.FileIDs = append(b.Manifest.FileIDs, id)
		}
	}
	sort.Slice(b.Manifest.Knowledge, func(i, j int) bool { return b.Manifest.Knowledge[i].SourceID < b.Manifest.Knowledge[j].SourceID })
	sort.Strings(b.Manifest.FileIDs)
	// A second list checks association counts and metadata again, without claiming a server snapshot.
	again, err := ListRaw(c, "/api/v1/knowledge/", false)
	if err != nil {
		return nil, err
	}
	if len(again) != len(listed) {
		return nil, fmt.Errorf("knowledge set changed during capture")
	}
	seen := map[string]bool{}
	for _, raw := range again {
		k, err := decodeKnowledge(raw, owner)
		if err != nil {
			return nil, err
		}
		original, ok := b.Metadata[k.ID]
		if !ok {
			return nil, fmt.Errorf("knowledge set changed")
		}
		d, _ := decodeKnowledge(original, owner)
		if k.Name != d.Name || k.Description != d.Description || k.FileCount == nil {
			return nil, fmt.Errorf("knowledge changed")
		}
		ids, err := AssociationIDs(c, k.ID, owner)
		if err != nil {
			return nil, err
		}
		for _, rec := range b.Manifest.Knowledge {
			if rec.SourceID == k.ID && (!equalIDs(ids, rec.FileIDs) || len(ids) != *k.FileCount) {
				return nil, fmt.Errorf("associations changed during capture")
			}
		}
		seen[k.ID] = true
	}
	if len(seen) != len(listed) {
		return nil, fmt.Errorf("knowledge set changed")
	}
	return b, b.Validate()
}
func checkFileOwner(f *filearchive.Bundle, owner string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Metadata, &raw); err != nil {
		return err
	}
	var id string
	if err := json.Unmarshal(raw["user_id"], &id); err != nil || id != owner {
		return fmt.Errorf("file %s not owned by source owner", f.Manifest.SourceID)
	}
	if _, ok := raw["access_control"]; ok {
		return fmt.Errorf("legacy file access_control unsupported")
	}
	if grants, ok := raw["access_grants"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(grants, &list) != nil || list == nil || len(list) > 0 {
			return fmt.Errorf("file access grants unsupported")
		}
	}
	return nil
}
func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (b *Bundle) Validate() error {
	if b.Manifest.SourceVersion != SupportedVersion {
		return fmt.Errorf("unsupported archive source version")
	}
	if b.Manifest.Version != 1 || (!validID(b.Manifest.SourceOwner) && !(b.Manifest.SourceOwner == "" && len(b.Manifest.Knowledge) == 0 && len(b.Manifest.FileIDs) == 0)) {
		return fmt.Errorf("unsupported knowledge bundle")
	}
	kbSeen := map[string]bool{}
	needed := map[string]bool{}
	files := map[string]bool{}
	for _, id := range b.Manifest.FileIDs {
		if !validID(id) || files[id] {
			return fmt.Errorf("invalid or duplicate file identity")
		}
		files[id] = true
		f, ok := b.Files[id]
		if !ok || f.Manifest.SourceID != id {
			return fmt.Errorf("file representation missing")
		}
		if err := f.Validate(); err != nil {
			return err
		}
		if f.Manifest.IndexedState != "present" {
			return fmt.Errorf("non-restorable indexed state")
		}
		if err := checkFileOwner(f, b.Manifest.SourceOwner); err != nil {
			return err
		}
	}
	if len(files) != len(b.Files) {
		return fmt.Errorf("unexpected file representations")
	}
	for _, rec := range b.Manifest.Knowledge {
		if !validID(rec.SourceID) || kbSeen[rec.SourceID] {
			return fmt.Errorf("invalid or duplicate knowledge identity")
		}
		kbSeen[rec.SourceID] = true
		raw, ok := b.Metadata[rec.SourceID]
		if !ok || hash(raw) != rec.MetadataSHA256 {
			return fmt.Errorf("knowledge metadata checksum mismatch")
		}
		k, err := decodeKnowledge(raw, b.Manifest.SourceOwner)
		if err != nil {
			return err
		}
		if k.ID != rec.SourceID {
			return fmt.Errorf("knowledge metadata identity mismatch")
		}
		seen := map[string]bool{}
		for _, id := range rec.FileIDs {
			if seen[id] || !files[id] {
				return fmt.Errorf("invalid association identity")
			}
			seen[id] = true
			needed[id] = true
		}
	}
	if len(kbSeen) != len(b.Metadata) || len(needed) != len(files) {
		return fmt.Errorf("unexpected metadata or unreferenced file")
	}
	return nil
}
func Write(w *zip.Writer, b *Bundle) error {
	if err := b.Validate(); err != nil {
		return err
	}
	manifest, err := json.MarshalIndent(b.Manifest, "", "  ")
	if err != nil {
		return err
	}
	write := func(name string, data []byte) error {
		f, err := w.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err = write("knowledge-manifest.json", manifest); err != nil {
		return err
	}
	for _, rec := range b.Manifest.Knowledge {
		if err = write("knowledge/"+rec.SourceID+".json", b.Metadata[rec.SourceID]); err != nil {
			return err
		}
	}
	for _, id := range b.Manifest.FileIDs {
		if err = filearchive.Write(w, "files/"+id+"/", b.Files[id]); err != nil {
			return err
		}
	}
	return nil
}
func Read(r *zip.Reader) (*Bundle, error) {
	if err := filearchive.ValidateArchivePaths(r); err != nil {
		return nil, err
	}
	entries := map[string]*zip.File{}
	for _, f := range r.File {
		entries[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := entries[name]
		if !ok {
			return nil, fmt.Errorf("missing %s; legacy knowledge archives cannot restore original uploads", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	raw, err := read("knowledge-manifest.json")
	if err != nil {
		return nil, err
	}
	b := &Bundle{Metadata: map[string]json.RawMessage{}, Files: map[string]*filearchive.Bundle{}}
	if err = json.Unmarshal(raw, &b.Manifest); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"knowledge-manifest.json": true, "owui.json": true}
	for _, rec := range b.Manifest.Knowledge {
		if !validID(rec.SourceID) {
			return nil, fmt.Errorf("invalid knowledge ID")
		}
		name := "knowledge/" + rec.SourceID + ".json"
		allowed[name] = true
		raw, err := read(name)
		if err != nil {
			return nil, err
		}
		b.Metadata[rec.SourceID] = raw
	}
	for _, id := range b.Manifest.FileIDs {
		if !validID(id) {
			return nil, fmt.Errorf("invalid file ID")
		}
		prefix := "files/" + id + "/"
		f, err := filearchive.Read(r, prefix)
		if err != nil {
			return nil, err
		}
		b.Files[id] = f
		for _, name := range []string{"representations.json", "file.json", "original.bin", "indexed.txt"} {
			allowed[prefix+name] = true
		}
	}
	for name := range entries {
		if !allowed[name] {
			return nil, fmt.Errorf("unexpected knowledge archive entry %s", name)
		}
	}
	return b, b.Validate()
}

// RequireVersion rejects unsupported API generations before capture or mutation.
// The version string is a compatibility gate, not proof of an exact build hash.
func RequireVersion(c *openwebui.Client) error {
	raw, err := c.ReadFileEndpoint("/api/version")
	if err != nil {
		return fmt.Errorf("version capability unavailable: %w", err)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if v.Version != SupportedVersion {
		return fmt.Errorf("unsupported Open WebUI version %q; this stage requires %s", v.Version, SupportedVersion)
	}
	return nil
}
