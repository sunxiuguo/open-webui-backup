package filearchive_test

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
	"strings"
	"testing"

	"github.com/vosiander/open-webui-backup/pkg/backup"
	"github.com/vosiander/open-webui-backup/pkg/filearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"github.com/vosiander/open-webui-backup/pkg/restore"
)

type fixture struct {
	original   []byte
	indexed    string
	data       any
	sourceFail string
	targetFail string
	status     int
	mismatch   string
	reuse      bool
	uploaded   []byte
	edited     string
	writes     int
	requests   []string
}

func (f *fixture) source(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("source mutation %s", r.Method)
		}
		if r.URL.Path == f.sourceFail {
			w.WriteHeader(f.status)
			return
		}
		switch r.URL.Path {
		case "/api/v1/files/":
			io.WriteString(w, `{"items":[{"id":"old","meta":{"name":"fixture.csv"}}],"total":1}`)
		case "/api/v1/files/old":
			json.NewEncoder(w).Encode(map[string]any{"id": "old", "filename": "fixture.csv", "data": f.data, "meta": map[string]any{"custom_preserved": "evidence"}, "unknown_future_field": 42})
		case "/api/v1/files/old/content":
			w.Write(f.original)
		case "/api/v1/files/old/data/content":
			json.NewEncoder(w).Encode(map[string]string{"content": f.indexed})
		default:
			t.Errorf("unexpected source route %s", r.URL)
			w.WriteHeader(404)
		}
	}))
}
func (f *fixture) target(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		if r.Method == "POST" {
			f.writes++
		}
		if r.URL.Path == f.targetFail && (r.URL.Path != "/api/v1/files/" || r.Method == "POST") {
			w.WriteHeader(f.status)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/files/":
			io.WriteString(w, `{"items":[{"id":"existing"}],"total":1}`)
		case r.Method == "POST" && r.URL.Path == "/api/v1/files/":
			if r.URL.Query().Get("process") != "true" || r.URL.Query().Get("process_in_background") != "false" {
				t.Error("not synchronous processing")
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			if header.Filename != "fixture.csv" {
				t.Errorf("filename %s", header.Filename)
			}
			f.uploaded, _ = io.ReadAll(file)
			id := "new"
			if f.reuse {
				id = "existing"
			}
			json.NewEncoder(w).Encode(map[string]string{"id": id})
		case r.Method == "POST" && r.URL.Path == "/api/v1/files/new/data/content/update":
			if !bytes.Equal(f.uploaded, f.original) {
				t.Error("original upload was not A before B update")
			}
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			f.edited = body.Content
			io.WriteString(w, `{}`)
		case r.Method == "GET" && r.URL.Path == "/api/v1/files/new/content":
			if f.mismatch == "original" {
				io.WriteString(w, "wrong")
			} else {
				w.Write(f.uploaded)
			}
		case r.Method == "GET" && r.URL.Path == "/api/v1/files/new/data/content":
			b := f.edited
			if f.mismatch == "indexed" {
				b = "wrong"
			}
			json.NewEncoder(w).Encode(map[string]string{"content": b})
		default:
			t.Errorf("unexpected target route %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
}
func makeArchive(t *testing.T, f *fixture, unified bool) (string, error) {
	t.Helper()
	s := f.source(t)
	defer s.Close()
	c := openwebui.NewClient(s.URL, "synthetic")
	dir := t.TempDir()
	if unified {
		path := filepath.Join(dir, "unified.zip")
		err := backup.BackupRepresentationFilesUnified(c, path)
		if err != nil {
			if _, stat := os.Stat(path); !os.IsNotExist(stat) {
				t.Error("failed unified archive was retained")
			}
		}
		return path, err
	}
	err := backup.BackupRepresentationFiles(c, dir)
	paths, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		if len(paths) != 0 {
			t.Error("failed standalone archive retained")
		}
		return "", err
	}
	if len(paths) != 1 {
		t.Fatalf("archives %v", paths)
	}
	return paths[0], nil
}
func restoreArchive(t *testing.T, f *fixture, path string, unified bool) error {
	t.Helper()
	s := f.target(t)
	defer s.Close()
	c := openwebui.NewClient(s.URL, "synthetic")
	if unified {
		return restore.RestoreRepresentationFilesUnified(c, path, false)
	}
	result, err := restore.RestoreFileWithResult(c, path, false)
	if err == nil && (result.SourceID != "old" || result.NewID != "new" || !result.Verified) {
		t.Errorf("mapping %+v", result)
	}
	return err
}
func standard() *fixture {
	return &fixture{original: []byte("name,count\nalpha,2\n"), indexed: "name: alpha\ncount: 3", data: map[string]any{"content": "name: alpha\ncount: 3"}}
}
func TestRealZIPRoundTrip(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for _, kind := range []string{"edited", "empty", "binary", "identical"} {
			t.Run(fmt.Sprintf("unified=%v/%s", unified, kind), func(t *testing.T) {
				f := standard()
				switch kind {
				case "empty":
					f.indexed = ""
				case "binary":
					f.original = []byte{0, 255, 1, 13, 10, 0, 128}
				case "identical":
					f.indexed = string(f.original)
				}
				f.data = map[string]any{"content": f.indexed}
				path, err := makeArchive(t, f, unified)
				if err != nil {
					t.Fatal(err)
				}
				r, err := zip.OpenReader(path)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				prefix := ""
				if unified {
					prefix = "files/old/"
				}
				b, err := filearchive.Read(&r.Reader, prefix)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(b.Original, f.original) || b.Indexed != f.indexed || !bytes.Contains(b.Metadata, []byte("unknown_future_field")) {
					t.Fatal("archive representation or raw metadata lost")
				}
				if kind == "empty" {
					if err = restoreArchive(t, f, path, unified); err == nil || f.writes != 0 {
						t.Fatalf("empty B must fail before mutation: %v writes=%d", err, f.writes)
					}
					return
				}
				if err = restoreArchive(t, f, path, unified); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(f.uploaded, f.original) || f.edited != f.indexed || f.writes != 2 {
					t.Fatalf("bad restore: %+v", f)
				}
			})
		}
	}
}
func TestSourceHTTPFailuresDoNotCreateArchive(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for _, route := range []string{"/api/v1/files/old/content", "/api/v1/files/old/data/content"} {
			for _, code := range []int{403, 404, 500} {
				t.Run(fmt.Sprintf("%v/%s/%d", unified, route, code), func(t *testing.T) {
					f := standard()
					f.sourceFail = route
					f.status = code
					if _, err := makeArchive(t, f, unified); err == nil {
						t.Fatal("expected error")
					}
				})
			}
		}
	}
}
func TestTargetFailuresNeverReportSuccess(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for _, route := range []string{"/api/v1/files/", "/api/v1/files/new/data/content/update", "/api/v1/files/new/content", "/api/v1/files/new/data/content"} {
			for _, code := range []int{403, 404, 500} {
				t.Run(fmt.Sprintf("%v/%s/%d", unified, route, code), func(t *testing.T) {
					f := standard()
					path, err := makeArchive(t, f, unified)
					if err != nil {
						t.Fatal(err)
					}
					f.targetFail = route
					f.status = code
					err = restoreArchive(t, f, path, unified)
					if err == nil {
						t.Fatal("expected error")
					}
					if route != "/api/v1/files/" && !strings.Contains(err.Error(), "created as new but not verified") {
						t.Errorf("lost partial ID: %v", err)
					}
				})
			}
		}
	}
}
func TestMissingNullRemainDistinctAndFailBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  any
		state string
	}{{"missing", map[string]any{}, "missing"}, {"null", map[string]any{"content": nil}, "null"}, {"null-data", nil, "null-data"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := standard()
			f.data = tc.data
			path, err := makeArchive(t, f, false)
			if err != nil {
				t.Fatal(err)
			}
			r, _ := zip.OpenReader(path)
			defer r.Close()
			b, err := filearchive.Read(&r.Reader, "")
			if err != nil {
				t.Fatal(err)
			}
			if b.Manifest.IndexedState != tc.state {
				t.Fatal(b.Manifest)
			}
			if err = restoreArchive(t, f, path, false); err == nil || f.writes != 0 {
				t.Fatalf("err=%v writes=%d", err, f.writes)
			}
		})
	}
}
func TestReadbackMismatchAndReusedID(t *testing.T) {
	for _, kind := range []string{"original", "indexed", "reuse"} {
		t.Run(kind, func(t *testing.T) {
			f := standard()
			path, err := makeArchive(t, f, false)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "reuse" {
				f.reuse = true
			} else {
				f.mismatch = kind
			}
			if err = restoreArchive(t, f, path, false); err == nil {
				t.Fatal("expected failure")
			}
			if f.reuse && f.writes != 1 {
				t.Fatal("updated existing upload")
			}
		})
	}
}
func TestLegacyAndTamperFailBeforeWrites(t *testing.T) {
	for _, kind := range []string{"legacy", "hash", "duplicate", "version"} {
		t.Run(kind, func(t *testing.T) {
			f := standard()
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			if kind == "legacy" {
				e, _ := w.Create("file.json")
				io.WriteString(e, `{"id":"old","filename":"fixture.csv","data":{"content":"B"}}`)
			} else {
				s := f.source(t)
				b, err := filearchive.Capture(openwebui.NewClient(s.URL, "synthetic"), "old")
				s.Close()
				if err != nil {
					t.Fatal(err)
				}
				if kind == "hash" {
					b.Manifest.OriginalSHA256 = "bad"
				}
				if kind == "version" {
					b.Manifest.Version = 99
				}
				m, _ := json.Marshal(b.Manifest)
				for name, data := range map[string][]byte{"representations.json": m, "file.json": b.Metadata, "original.bin": b.Original, "indexed.txt": []byte(b.Indexed)} {
					e, _ := w.Create(name)
					e.Write(data)
				}
				if kind == "duplicate" {
					e, _ := w.Create("original.bin")
					e.Write(b.Original)
				}
			}
			w.Close()
			path := filepath.Join(t.TempDir(), "bad.zip")
			os.WriteFile(path, buf.Bytes(), 0600)
			if err := restoreArchive(t, f, path, false); err == nil || f.writes != 0 {
				t.Fatalf("err=%v writes=%d", err, f.writes)
			}
		})
	}
}
func TestListFilesPagination(t *testing.T) {
	for _, mode := range []string{"normal", "legacy", "empty", "duplicate", "changing", "invalid", "null"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "legacy" {
					io.WriteString(w, `[{"id":"a"},{"id":"b"}]`)
					return
				}
				if mode == "invalid" {
					io.WriteString(w, `{}`)
					return
				}
				if mode == "null" {
					io.WriteString(w, `null`)
					return
				}
				if calls == 1 {
					io.WriteString(w, `{"items":[{"id":"a"}],"total":2}`)
					return
				}
				if calls > 2 {
					t.Error("unbounded pagination")
				}
				if r.URL.Query().Get("page") != "2" {
					t.Error("incorrect page")
				}
				switch mode {
				case "normal":
					io.WriteString(w, `{"items":[{"id":"b"}],"total":2}`)
				case "empty":
					io.WriteString(w, `{"items":[],"total":2}`)
				case "duplicate":
					io.WriteString(w, `{"items":[{"id":"a"}],"total":2}`)
				case "changing":
					io.WriteString(w, `{"items":[{"id":"b"}],"total":3}`)
				}
			}))
			defer s.Close()
			files, err := openwebui.NewClient(s.URL, "synthetic").ListRepresentationFiles()
			if mode == "normal" || mode == "legacy" {
				if err != nil || len(files) != 2 {
					t.Fatalf("%v %v", files, err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

func TestMalformedArchivePathsFailBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"traversal", "mismatch", "duplicate-id", "empty", "extra", "absolute", "backslash", "descendant-traversal", "duplicate-metadata"} {
		t.Run(kind, func(t *testing.T) {
			f := standard()
			s := f.source(t)
			b, err := filearchive.Capture(openwebui.NewClient(s.URL, "synthetic"), "old")
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			prefix := "files/old/"
			switch kind {
			case "traversal":
				prefix = "files/../"
			case "mismatch":
				prefix = "files/other/"
			}
			if kind != "empty" {
				if err = filearchive.Write(w, prefix, b); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "duplicate-id" {
				filearchive.Write(w, "files/other/", b)
			}
			extra := ""
			switch kind {
			case "extra":
				extra = "files/old/content/pretend.csv"
			case "descendant-traversal":
				extra = "files/old/../escape"
			case "duplicate-metadata":
				extra = "owui.json"
			case "absolute":
				extra = "/unexpected"
			case "backslash":
				extra = "files\\old\\extra"
			}
			if extra != "" {
				e, _ := w.Create(extra)
				io.WriteString(e, "extra")
			}
			e, _ := w.Create("owui.json")
			io.WriteString(e, `{"unified_backup":true,"contained_types":["file"],"item_count":1}`)
			w.Close()
			path := filepath.Join(t.TempDir(), "malformed.zip")
			os.WriteFile(path, buf.Bytes(), 0600)
			if err = restoreArchive(t, f, path, true); err == nil || len(f.requests) != 0 {
				t.Fatalf("error=%v requests=%v", err, f.requests)
			}
		})
	}
}

func TestUploadFailurePreservesError(t *testing.T) {
	f := standard()
	path, err := makeArchive(t, f, false)
	if err != nil {
		t.Fatal(err)
	}
	f.targetFail = "/api/v1/files/"
	f.status = 500
	if err = restoreArchive(t, f, path, false); err == nil || f.writes != 1 {
		t.Fatalf("error=%v writes=%d", err, f.writes)
	}
}

func TestUnifiedMetadataCannotHideFiles(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			f := standard()
			s := f.source(t)
			b, err := filearchive.Capture(openwebui.NewClient(s.URL, "synthetic"), "old")
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			filearchive.Write(w, "files/old/", b)
			e, _ := w.Create("owui.json")
			io.WriteString(e, `{"unified_backup":true,"contained_types":[],"item_count":0}`)
			if duplicate {
				e, _ = w.Create("owui.json")
				io.WriteString(e, `{"unified_backup":true,"contained_types":["file"],"item_count":1}`)
			}
			w.Close()
			path := filepath.Join(t.TempDir(), "bad.zip")
			os.WriteFile(path, buf.Bytes(), 0600)
			if err = restoreArchive(t, f, path, true); err == nil || len(f.requests) != 0 {
				t.Fatalf("err=%v requests=%v", err, f.requests)
			}
		})
	}
}
func TestRepresentationRestorePropagatesFileFailure(t *testing.T) {
	f := standard()
	path, err := makeArchive(t, f, false)
	if err != nil {
		t.Fatal(err)
	}
	f.targetFail = "/api/v1/files/new/data/content/update"
	f.status = 500
	s := f.target(t)
	defer s.Close()
	if err = restore.RestoreRepresentationFile(openwebui.NewClient(s.URL, "synthetic"), path, false); err == nil {
		t.Fatal("RestoreAll concealed file failure")
	}
}
