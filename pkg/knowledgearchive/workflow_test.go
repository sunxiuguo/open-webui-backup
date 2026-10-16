package knowledgearchive_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/vosiander/open-webui-backup/pkg/backup"
	"github.com/vosiander/open-webui-backup/pkg/knowledgearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"github.com/vosiander/open-webui-backup/pkg/restore"
)

type testFile struct {
	ID       string
	Owner    string
	Original []byte
	Text     string
	Filename string
}
type testKB struct {
	ID, Owner, Name string
	Files           []string
	Grants          []any
	Meta            any
}
type service struct {
	t             *testing.T
	source        bool
	mode          string
	kbs           map[string]*testKB
	files         map[string]*testFile
	requests      []string
	writes        int
	uploads       int
	creates       int
	updates       int
	links         int
	originalReads map[string]int
	reportDir     string
}

func newService(t *testing.T, source bool) *service {
	s := &service{t: t, source: source, kbs: map[string]*testKB{}, files: map[string]*testFile{}, originalReads: map[string]int{}}
	if source {
		s.files["fa"] = &testFile{"fa", "source-owner", []byte{0, 255, 13, 10, 1}, "edited A", "same.csv"}
		s.files["fb"] = &testFile{"fb", "source-owner", []byte("original B\n"), "edited B", "same.csv"}
		s.kbs["ka"] = &testKB{"ka", "source-owner", "Alpha", []string{"fa", "fb"}, []any{}, nil}
		s.kbs["kb"] = &testKB{"kb", "source-owner", "Beta", []string{"fa"}, []any{}, nil}
	}
	return s
}
func (s *service) kbRaw(k *testKB, list bool) map[string]any {
	raw := map[string]any{"id": k.ID, "user_id": k.Owner, "name": k.Name, "description": "synthetic description", "access_grants": k.Grants, "meta": k.Meta}
	if list {
		raw["file_count"] = len(k.Files)
	}
	if s.source && s.mode == "missing-grants" {
		delete(raw, "access_grants")
	}
	if s.source && s.mode == "legacy-acl" {
		raw["access_control"] = map[string]any{}
	}
	return raw
}
func (s *service) fileRaw(f *testFile) map[string]any {
	data := map[string]any{"content": f.Text}
	if s.mode == "missing-text" && s.source {
		data = map[string]any{}
	}
	return map[string]any{"id": f.ID, "user_id": f.Owner, "filename": f.Filename, "meta": map[string]any{"name": f.Filename}, "data": data}
}
func (s *service) page(w http.ResponseWriter, r *http.Request, items []any, assoc bool) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		s.t.Errorf("page missing: %s", r.URL)
		page = 1
	}
	out := []any{}
	if page <= len(items) {
		out = append(out, items[page-1])
	}
	total := len(items)
	if s.source && s.mode == "pagination-empty" && page == 2 {
		out = []any{}
	}
	if s.source && s.mode == "pagination-duplicate" && page == 2 && len(items) > 0 {
		out = []any{items[0]}
	}
	raw := map[string]any{"items": out, "total": total}
	if assoc {
		raw["directories"] = []any{}
		raw["breadcrumbs"] = []any{}
		if s.source && s.mode == "empty-directory" {
			raw["directories"] = []any{map[string]any{"id": "dir"}}
		}
	}
	json.NewEncoder(w).Encode(raw)
}
func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	if r.Method != "GET" {
		s.writes++
		if s.source {
			s.t.Error("source mutation")
		}
		if s.reportDir != "" {
			paths, _ := filepath.Glob(filepath.Join(s.reportDir, "knowledge-restore-*.json"))
			if len(paths) != 1 {
				s.t.Error("mutation without durable report")
			}
		}
	}
	if s.mode == "endpoint-403" && r.URL.Path == "/api/v1/knowledge/search" {
		w.WriteHeader(403)
		return
	}
	p := r.URL.Path
	if p == "/api/version" && r.Method == "GET" {
		switch s.mode {
		case "old-version":
			io.WriteString(w, `{"version":"0.6.0"}`)
		case "unknown-version":
			io.WriteString(w, `{}`)
		case "version-403":
			w.WriteHeader(403)
		default:
			io.WriteString(w, `{"version":"0.12.2"}`)
		}
		return
	}
	if r.Method == "GET" && (p == "/api/v1/knowledge/" || p == "/api/v1/knowledge/search") {
		ids := []string{}
		for id := range s.kbs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		items := []any{}
		for _, id := range ids {
			k := s.kbs[id]
			if p == "/api/v1/knowledge/search" {
				if r.URL.Query().Get("view_option") != "created" {
					s.t.Error("ownership filter missing")
				}
				if s.source && k.Owner != "source-owner" {
					continue
				}
			}
			items = append(items, s.kbRaw(k, true))
		}
		s.page(w, r, items, false)
		return
	}
	if r.Method == "POST" && p == "/api/v1/knowledge/create" {
		s.creates++
		if s.mode == "create-500" {
			w.WriteHeader(500)
			return
		}
		if s.mode == "create-null" {
			io.WriteString(w, "null")
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Grants      *[]any `json:"access_grants"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Error(err)
		}
		if body.Grants == nil || len(*body.Grants) != 0 {
			s.t.Error("not explicit private create")
		}
		id := fmt.Sprintf("new-kb-%d", s.creates)
		if s.mode == "reuse-kb" {
			id = "existing"
		}
		k := &testKB{id, "target-owner", body.Name, []string{}, []any{}, nil}
		if s.mode == "grant-drift" {
			k.Grants = []any{map[string]any{"principal_type": "anyone"}}
		}
		s.kbs[id] = k
		json.NewEncoder(w).Encode(s.kbRaw(k, false))
		return
	}
	if strings.HasPrefix(p, "/api/v1/knowledge/") {
		parts := strings.Split(strings.TrimPrefix(p, "/api/v1/knowledge/"), "/")
		k := s.kbs[parts[0]]
		if k == nil {
			w.WriteHeader(404)
			return
		}
		if len(parts) == 1 && r.Method == "GET" {
			json.NewEncoder(w).Encode(s.kbRaw(k, false))
			return
		}
		if len(parts) == 2 && parts[1] == "files" && r.Method == "GET" {
			items := []any{}
			for _, id := range k.Files {
				f := s.files[id]
				raw := map[string]any{"id": id, "user_id": f.Owner, "directory_id": nil, "directory_path": "", "has_original": true}
				if s.source && s.mode == "nonroot" {
					raw["directory_id"] = "dir"
					raw["directory_path"] = "Folder"
				}
				items = append(items, raw)
			}
			if s.source && s.mode == "swallowed-list-error" {
				items = []any{}
			}
			s.page(w, r, items, true)
			return
		}
		if len(parts) == 3 && parts[1] == "file" && parts[2] == "add" && r.Method == "POST" {
			s.links++
			if s.mode == "link-500" {
				w.WriteHeader(500)
				return
			}
			var form map[string]any
			json.NewDecoder(r.Body).Decode(&form)
			if form["directory_id"] != nil {
				s.t.Error("nonroot link")
			}
			id, _ := form["file_id"].(string)
			if s.files[id] == nil {
				s.t.Error("link uses unknown file")
			}
			if s.mode != "link-noop" {
				k.Files = append(k.Files, id)
			}
			io.WriteString(w, "null")
			return
		}
	}
	if r.Method == "GET" && p == "/api/v1/files/" {
		ids := []string{}
		for id := range s.files {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		items := []any{}
		for _, id := range ids {
			items = append(items, map[string]any{"id": id})
		}
		s.page(w, r, items, false)
		return
	}
	if r.Method == "POST" && p == "/api/v1/files/" {
		s.uploads++
		if s.mode == "upload-500" {
			w.WriteHeader(500)
			return
		}
		if r.URL.Query().Get("process") != "true" || r.URL.Query().Get("process_in_background") != "false" {
			s.t.Error("upload not synchronous")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			s.t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, err := r.FormFile("file")
		if err != nil {
			s.t.Error(err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		id := fmt.Sprintf("new-file-%d", s.uploads)
		s.files[id] = &testFile{id, "target-owner", data, "extracted automatically", h.Filename}
		json.NewEncoder(w).Encode(map[string]any{"id": id})
		return
	}
	if strings.HasPrefix(p, "/api/v1/files/") {
		rest := strings.TrimPrefix(p, "/api/v1/files/")
		parts := strings.Split(rest, "/")
		f := s.files[parts[0]]
		if f == nil {
			w.WriteHeader(404)
			return
		}
		if len(parts) == 1 && r.Method == "GET" {
			if s.source && s.mode == "metadata-403" {
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(s.fileRaw(f))
			return
		}
		suffix := strings.Join(parts[1:], "/")
		if suffix == "content" && r.Method == "GET" {
			s.originalReads[f.ID]++
			if s.source && s.mode == "original-404" {
				w.WriteHeader(404)
				return
			}
			if s.source && s.mode == "original-403" {
				w.WriteHeader(403)
				return
			}
			if !s.source && s.mode == "readback-mismatch" {
				io.WriteString(w, "wrong")
				return
			}
			w.Write(f.Original)
			return
		}
		if suffix == "data/content" && r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]string{"content": f.Text})
			return
		}
		if suffix == "data/content/update" && r.Method == "POST" {
			s.updates++
			if s.mode == "update-500" {
				w.WriteHeader(500)
				return
			}
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.Text = body.Content
			json.NewEncoder(w).Encode(map[string]string{"content": f.Text})
			return
		}
	}
	s.t.Errorf("unexpected route %s %s", r.Method, r.URL)
	w.WriteHeader(404)
}
func (s *service) server() *httptest.Server { return httptest.NewServer(http.HandlerFunc(s.serve)) }
func capture(t *testing.T, s *service, selective bool) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if root := os.Getenv("KB_EVIDENCE_DIR"); root != "" {
		dir = filepath.Join(root, strings.ReplaceAll(t.Name(), "/", "_"))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	server := s.server()
	defer server.Close()
	c := openwebui.NewClient(server.URL, "synthetic")
	if selective {
		path := filepath.Join(dir, "knowledge.zip")
		err := backup.BackupModernKnowledgeFile(c, path, true)
		return path, err
	}
	err := backup.BackupModernKnowledge(c, dir)
	paths, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		if len(paths) != 0 {
			t.Error("incomplete capture left ZIP")
		}
		return "", err
	}
	if len(paths) != 1 {
		t.Fatalf("archives %v", paths)
	}
	return paths[0], nil
}
func loadBundle(t *testing.T, path string) *knowledgearchive.Bundle {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := knowledgearchive.Read(&r.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestPublicKnowledgeWorkflow(t *testing.T) {
	for _, selective := range []bool{false, true} {
		t.Run(fmt.Sprint(selective), func(t *testing.T) {
			src := newService(t, true)
			path, err := capture(t, src, selective)
			if err != nil {
				t.Fatal(err)
			}
			b := loadBundle(t, path)
			if len(b.Manifest.Knowledge) != 2 || len(b.Files) != 2 {
				t.Fatal("dedup missing")
			}
			for id, f := range src.files {
				if src.originalReads[id] != 1 || !bytes.Equal(b.Files[id].Original, f.Original) || b.Files[id].Indexed != f.Text {
					t.Fatalf("capture changed file %s", id)
				}
			}
			dst := newService(t, false)
			dst.reportDir = filepath.Dir(path)
			server := dst.server()
			defer server.Close()
			c := openwebui.NewClient(server.URL, "synthetic")
			var report knowledgearchive.Report
			var reportPath string
			if selective {
				err = restore.RestoreModernKnowledge(c, path, false)
				paths, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "knowledge-restore-*.json"))
				if len(paths) != 1 {
					t.Fatal("report absent")
				}
				reportPath = paths[0]
			} else {
				reportPath, report, err = restore.RestoreModernKnowledgeWithReport(c, path, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(reportPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			if report.Status != "complete" || len(report.Files) != 2 || len(report.Knowledge) != 2 || dst.uploads != 2 || dst.links != 3 || dst.updates != 2 {
				t.Fatalf("bad result %+v uploads=%d links=%d", report, dst.uploads, dst.links)
			}
			for old, id := range report.Files {
				got := dst.files[id]
				want := src.files[old]
				if got.Filename != want.Filename || got.Text != want.Text || !bytes.Equal(got.Original, want.Original) {
					t.Errorf("representation lost %s", old)
				}
			}
			for _, rec := range b.Manifest.Knowledge {
				got := dst.kbs[report.Knowledge[rec.SourceID]]
				expected := []string{}
				for _, old := range rec.FileIDs {
					expected = append(expected, report.Files[old])
				}
				sort.Strings(expected)
				sort.Strings(got.Files)
				if fmt.Sprint(expected) != fmt.Sprint(got.Files) {
					t.Error("wrong graph")
				}
				if len(got.Grants) != 0 {
					t.Error("privacy changed")
				}
			}
			if os.Getenv("KB_EVIDENCE_DIR") != "" {
				evidence, _ := json.MarshalIndent(map[string]any{"source_requests": src.requests, "target_requests": dst.requests, "report": report, "uploads": dst.uploads, "links": dst.links}, "", "  ")
				if e := os.WriteFile(filepath.Join(filepath.Dir(path), "e2e-evidence.json"), evidence, 0600); e != nil {
					t.Fatal(e)
				}
			}
			info, _ := os.Stat(reportPath)
			if info.Mode().Perm() != 0600 {
				t.Error("report not private")
			}
		})
	}
}
func TestCaptureFailsUnsupportedOrIncomplete(t *testing.T) {
	for _, mode := range []string{"grants", "foreign-kb", "foreign-file", "missing-grants", "legacy-acl", "external", "nonroot", "empty-directory", "missing-text", "metadata-403", "original-403", "original-404", "pagination-empty", "pagination-duplicate", "swallowed-list-error", "endpoint-403", "old-version", "unknown-version", "version-403"} {
		t.Run(mode, func(t *testing.T) {
			src := newService(t, true)
			src.mode = mode
			switch mode {
			case "grants":
				src.kbs["ka"].Grants = []any{"anyone"}
			case "foreign-kb":
				src.kbs["ka"].Owner = "other"
			case "foreign-file":
				src.files["fa"].Owner = "other"
			case "external":
				src.kbs["ka"].Meta = map[string]any{"source": "external"}
			}
			if _, err := capture(t, src, false); err == nil {
				t.Fatal("unsupported capture succeeded")
			}
			if src.writes != 0 {
				t.Error("source mutated")
			}
		})
	}
}
func TestRestorePartialResults(t *testing.T) {
	for _, mode := range []string{"endpoint-403", "old-version", "unknown-version", "version-403", "create-500", "create-null", "grant-drift", "reuse-kb", "upload-500", "update-500", "readback-mismatch", "link-500", "link-noop"} {
		t.Run(mode, func(t *testing.T) {
			src := newService(t, true)
			path, err := capture(t, src, false)
			if err != nil {
				t.Fatal(err)
			}
			dst := newService(t, false)
			dst.mode = mode
			dst.reportDir = filepath.Dir(path)
			if mode == "reuse-kb" {
				dst.kbs["existing"] = &testKB{"existing", "target-owner", "existing", []string{}, []any{}, nil}
			}
			server := dst.server()
			defer server.Close()
			reportPath, report, err := restore.RestoreModernKnowledgeWithReport(openwebui.NewClient(server.URL, "synthetic"), path, false)
			if err == nil {
				t.Fatal("expected incomplete outcome")
			}
			if mode == "endpoint-403" || mode == "old-version" || mode == "unknown-version" || mode == "version-403" {
				if dst.writes != 0 || reportPath != "" {
					t.Fatal("preflight wrote")
				}
				return
			}
			if report.Status != "incomplete" || reportPath == "" {
				t.Fatalf("missing partial report %+v %v", report, err)
			}
			raw, e := os.ReadFile(reportPath)
			if e != nil {
				t.Fatal(e)
			}
			var persisted knowledgearchive.Report
			json.Unmarshal(raw, &persisted)
			if persisted.Status != "incomplete" || len(persisted.Attempts) == 0 {
				t.Fatal("report incomplete")
			}
			if mode == "update-500" && len(persisted.Files) != 1 {
				t.Error("created upload ID was not durable")
			}
			if mode == "link-noop" && dst.links != 1 {
				t.Error("continued after no-op link")
			}
			if mode == "upload-500" && dst.uploads != 1 {
				t.Error("retried uncertain upload")
			}
		})
	}
}
func TestPreflightRejectsArchiveAndUnsupportedEntrypoints(t *testing.T) {
	src := newService(t, true)
	path, err := capture(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	dst := newService(t, false)
	server := dst.server()
	defer server.Close()
	c := openwebui.NewClient(server.URL, "synthetic")
	if err = restore.RestoreModernKnowledge(c, path, true); err == nil {
		t.Error("overwrite accepted")
	}
	if err = restore.RestoreSelective(c, path, &restore.SelectiveRestoreOptions{Knowledge: true, Files: true}, false, nil); err == nil {
		t.Error("mixed restore accepted")
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("knowledge_base.json")
	io.WriteString(f, `{"id":"legacy"}`)
	w.Close()
	legacy := filepath.Join(t.TempDir(), "legacy.zip")
	os.WriteFile(legacy, buf.Bytes(), 0600)
	if err = restore.RestoreModernKnowledge(c, legacy, false); err == nil {
		t.Error("legacy accepted")
	}
	if len(dst.requests) != 0 {
		t.Error("unsupported preflight made network requests")
	}
}

func TestArchiveValidationBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"checksum", "duplicate-id", "missing-file", "extra-file", "duplicate-metadata", "traversal", "legacy-grant"} {
		t.Run(kind, func(t *testing.T) {
			src := newService(t, true)
			path, err := capture(t, src, false)
			if err != nil {
				t.Fatal(err)
			}
			r, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			for _, entry := range r.File {
				rc, _ := entry.Open()
				data, _ := io.ReadAll(rc)
				rc.Close()
				name := entry.Name
				if kind == "missing-file" && name == "files/fa/original.bin" {
					continue
				}
				if kind == "checksum" && name == "files/fa/original.bin" {
					data = []byte("tampered")
				}
				if kind == "duplicate-id" && name == "knowledge-manifest.json" {
					var m knowledgearchive.Manifest
					json.Unmarshal(data, &m)
					m.FileIDs = append(m.FileIDs, m.FileIDs[0])
					data, _ = json.Marshal(m)
				}
				if kind == "legacy-grant" && name == "knowledge/ka.json" {
					var m map[string]any
					json.Unmarshal(data, &m)
					m["access_control"] = map[string]any{}
					data, _ = json.Marshal(m)
				}
				f, _ := w.Create(name)
				f.Write(data)
			}
			r.Close()
			extra := ""
			switch kind {
			case "extra-file":
				extra = "files/ghost/original.bin"
			case "duplicate-metadata":
				extra = "owui.json"
			case "traversal":
				extra = "knowledge/../escape"
			}
			if extra != "" {
				f, _ := w.Create(extra)
				io.WriteString(f, "{}")
			}
			w.Close()
			bad := filepath.Join(t.TempDir(), "bad.zip")
			os.WriteFile(bad, buf.Bytes(), 0600)
			dst := newService(t, false)
			server := dst.server()
			defer server.Close()
			if err = restore.RestoreModernKnowledge(openwebui.NewClient(server.URL, "synthetic"), bad, false); err == nil || len(dst.requests) != 0 {
				t.Fatalf("error=%v requests=%v", err, dst.requests)
			}
		})
	}
}
func TestReportCreationFailureBeforeMutation(t *testing.T) {
	src := newService(t, true)
	path, err := capture(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	b := loadBundle(t, path)
	dst := newService(t, false)
	server := dst.server()
	defer server.Close()
	_, _, err = knowledgearchive.Restore(openwebui.NewClient(server.URL, "synthetic"), b, filepath.Join(t.TempDir(), "missing"))
	if err == nil || dst.writes != 0 {
		t.Fatalf("err=%v writes=%d", err, dst.writes)
	}
}
func TestPublicListKnowledgePagination(t *testing.T) {
	s := newService(t, true)
	server := s.server()
	defer server.Close()
	items, err := openwebui.NewClient(server.URL, "synthetic").ListModernKnowledge()
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}
func TestEmptyKnowledgeBundleDoesNotMutate(t *testing.T) {
	src := newService(t, true)
	src.kbs = map[string]*testKB{}
	path, err := capture(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	dst := newService(t, false)
	server := dst.server()
	defer server.Close()
	_, report, err := restore.RestoreModernKnowledgeWithReport(openwebui.NewClient(server.URL, "synthetic"), path, false)
	if err != nil || report.Status != "complete-empty" || len(dst.requests) != 0 {
		t.Fatalf("report=%+v err=%v requests=%v", report, err, dst.requests)
	}
}

func TestEmptyIndexedTextArchivedButRestoreRejected(t *testing.T) {
	src := newService(t, true)
	src.files["fb"].Text = ""
	path, err := capture(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle := loadBundle(t, path)
	if bundle.Files["fb"].Indexed != "" || bundle.Files["fb"].Manifest.IndexedState != "present" {
		t.Fatal("empty text not preserved in archive")
	}
	dst := newService(t, false)
	server := dst.server()
	defer server.Close()
	_, _, err = restore.RestoreModernKnowledgeWithReport(openwebui.NewClient(server.URL, "synthetic"), path, false)
	if err == nil || len(dst.requests) != 0 {
		t.Fatalf("empty text restore not rejected before network: %v %v", err, dst.requests)
	}
}

func TestRestoreAllKnowledgeMixedDirectoryPreflight(t *testing.T) {
	src := newService(t, true)
	path, err := capture(t, src, false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("model.json")
	io.WriteString(f, `{"id":"model","name":"Synthetic model"}`)
	w.Close()
	os.WriteFile(filepath.Join(filepath.Dir(path), "synthetic_model_model.zip"), buf.Bytes(), 0600)
	dst := newService(t, false)
	server := dst.server()
	defer server.Close()
	c := openwebui.NewClient(server.URL, "synthetic")
	if err = restore.RestoreAll(c, filepath.Dir(path), false); err == nil || len(dst.requests) != 0 {
		t.Fatalf("mixed directory attempted work: %v %v", err, dst.requests)
	}
	if err = restore.RestoreSelective(c, path, &restore.SelectiveRestoreOptions{Files: true}, false, nil); err == nil || len(dst.requests) != 0 {
		t.Fatalf("KB partial extraction attempted work: %v %v", err, dst.requests)
	}
}
