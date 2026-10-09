package restore

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

type refusalEvent struct {
	Method       string
	Path         string
	Result       string
	AfterRefusal bool
}
type refusalFixture struct {
	mu              sync.Mutex
	events          []refusalEvent
	armed           bool
	refused         bool
	existing        bool
	failure         string
	ordinaryFailure bool
}

func (f *refusalFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := refusalEvent{Method: r.Method, Path: r.URL.Path, AfterRefusal: f.refused}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/version" {
		if f.armed && f.failure != "" {
			f.refused = true
			e.Result = f.failure
			f.events = append(f.events, e)
			switch f.failure {
			case "modern":
				fmt.Fprint(w, `{"version":"0.11.4"}`)
			case "unavailable":
				w.WriteHeader(503)
				fmt.Fprint(w, `{}`)
			case "malformed":
				fmt.Fprint(w, `<html>`)
			case "oversized":
				fmt.Fprint(w, strings.Repeat(" ", 4097)+`{"version":"0.6.18"}`)
			case "transport":
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					panic(err)
				}
				conn.Close()
			case "read":
				w.Header().Set("Content-Length", "100")
				w.Header().Set("Connection", "close")
				fmt.Fprint(w, `{"version":`)
			}
			return
		}
		e.Result = "0.6.18"
		f.events = append(f.events, e)
		fmt.Fprint(w, `{"version":"0.6.18"}`)
		return
	}
	if r.URL.Path == "/api/v1/knowledge/list" {
		f.armed = true
	}
	f.events = append(f.events, e)
	switch {
	case r.URL.Path == "/api/v1/knowledge/list":
		if f.existing {
			fmt.Fprint(w, `[{"id":"existing","name":"synthetic"}]`)
		} else {
			fmt.Fprint(w, `[]`)
		}
	case r.URL.Path == "/api/v1/knowledge/existing":
		fmt.Fprint(w, `{"id":"existing","name":"synthetic","description":"before","access_control":null,"files":[]}`)
	case r.URL.Path == "/api/v1/prompts/":
		fmt.Fprint(w, `[]`)
	case r.Method == "GET":
		http.Error(w, "synthetic missing item", 404)
	case f.ordinaryFailure && (r.URL.Path == "/api/v1/knowledge/create" || strings.HasSuffix(r.URL.Path, "/update")):
		http.Error(w, "synthetic resource error", 503)
	default:
		if r.URL.Path == "/api/v1/files/" {
			if err := r.ParseMultipartForm(1024); err != nil {
				panic(err)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				panic(err)
			}
			body, err := io.ReadAll(file)
			file.Close()
			if err != nil || len(body) == 0 {
				panic("missing synthetic document")
			}
			defer r.MultipartForm.RemoveAll()
		}
		fmt.Fprint(w, `{"id":"restored"}`)
	}
}
func (f *refusalFixture) calls() []refusalEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]refusalEvent(nil), f.events...)
}
func refusalZip(t *testing.T, dir, name string, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(file)
	for name, body := range entries {
		e, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const refusalKnowledge = `{"id":"source","name":"synthetic","description":"archive","access_control":{"read":{"group_ids":["original-group"],"user_ids":["original-user"]}}}`

func runRefusalRestore(t *testing.T, f *refusalFixture, mode string) error {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	defer s.Close()
	client := openwebui.NewClient(s.URL, "")
	dir := t.TempDir()
	model := `{"id":"synthetic-model","name":"synthetic"}`
	prompt := `{"command":"synthetic","title":"synthetic","content":"synthetic"}`
	collection := map[string]string{
		"knowledge-bases/source/knowledge_base.json":     refusalKnowledge,
		"knowledge-bases/source/documents/synthetic.txt": "synthetic collection document",
	}
	if mode == "knowledge" || mode == "legacy" {
		collection = map[string]string{"knowledge_base.json": refusalKnowledge, "documents/synthetic.txt": "synthetic collection document"}
	}
	if mode == "model" || mode == "legacy-model" {
		collection["model.json"] = model
		// This file is restored before the collection gate. No rollback is claimed.
		collection["model-files/embedded/metadata.json"] = `{"id":"embedded"}`
		collection["model-files/embedded/embedded.txt"] = "synthetic model document"
	}
	switch mode {
	case "knowledge":
		return RestoreKnowledge(client, refusalZip(t, dir, "synthetic.zip", collection), true)
	case "model":
		return RestoreModel(client, refusalZip(t, dir, "synthetic.zip", collection), true)
	case "selective", "unified":
		// A unified prompt write precedes the knowledge gate; model import follows it.
		collection["owui.json"] = `{"unified_backup":true,"contained_types":["prompt","knowledge","model"]}`
		collection["prompts/synthetic/prompt.json"] = prompt
		collection["models/synthetic/model.json"] = model
		refusalZip(t, dir, "synthetic.zip", collection)
		if mode == "selective" {
			return RestoreSelective(client, filepath.Join(dir, "synthetic.zip"), &SelectiveRestoreOptions{Knowledge: true, Models: true, Prompts: true}, true, nil)
		}
		return RestoreAll(client, dir, true)
	case "legacy", "legacy-model":
		name := "synthetic_knowledge_base_test.zip"
		if mode == "legacy-model" {
			name = "synthetic_model_test.zip"
		}
		refusalZip(t, dir, name, collection)
		refusalZip(t, dir, "synthetic_prompt_test.zip", map[string]string{"prompt.json": prompt})
		return RestoreAll(client, dir, true)
	default:
		t.Fatal("unknown mode")
		return nil
	}
}
func refusalTrace(t *testing.T, f *refusalFixture, err error) []refusalEvent {
	t.Helper()
	calls := f.calls()
	returned := "<nil>"
	if err != nil {
		returned = regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`).ReplaceAllString(err.Error(), "http://synthetic-loopback")
	}
	trace := struct {
		ReturnedError string
		Requests      []refusalEvent
	}{returned, calls}
	bytes, _ := json.Marshal(trace)
	t.Log("REFUSAL_TRACE " + string(bytes))
	return calls
}
func TestKnowledgeRefusalStopsRestoreOperation(t *testing.T) {
	for _, mode := range []string{"knowledge", "model", "selective", "unified", "legacy", "legacy-model"} {
		for _, existing := range []bool{false, true} {
			for _, failure := range []string{"modern", "unavailable", "malformed", "oversized", "transport", "read"} {
				t.Run(fmt.Sprintf("%s/existing=%v/%s", mode, existing, failure), func(t *testing.T) {
					f := &refusalFixture{existing: existing, failure: failure}
					err := runRefusalRestore(t, f, mode)
					calls := refusalTrace(t, f, err)
					found := false
					for _, c := range calls {
						if c.Result == failure {
							found = true
						}
						if c.AfterRefusal && c.Method != "GET" {
							t.Errorf("write continued after refusal: %s %s", c.Method, c.Path)
						}
					}
					if !found {
						t.Fatal("fixture did not reach mutation compatibility recheck")
					}
					if err == nil {
						t.Error("restore reported success after compatibility refusal")
					}
				})
			}
		}
	}
}
func TestKnowledgeRefusalLegacyPositiveControls(t *testing.T) {
	for _, mode := range []string{"knowledge", "model", "selective", "unified", "legacy", "legacy-model"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", mode, existing), func(t *testing.T) {
				f := &refusalFixture{existing: existing}
				err := runRefusalRestore(t, f, mode)
				calls := refusalTrace(t, f, err)
				if err != nil {
					t.Fatal("legacy positive restore failed")
				}
				hasKnowledge, hasUpload, hasLink := false, false, false
				for _, c := range calls {
					if c.Method == "GET" {
						continue
					}
					if c.Path == "/api/v1/knowledge/create" || c.Path == "/api/v1/knowledge/existing/update" {
						hasKnowledge = true
					}
					if c.Path == "/api/v1/files/" {
						hasUpload = true
					}
					if strings.HasSuffix(c.Path, "/file/add") {
						hasLink = true
					}
				}
				if !hasKnowledge || !hasUpload || !hasLink {
					t.Fatalf("positive controls incomplete: mutation=%v upload=%v link=%v", hasKnowledge, hasUpload, hasLink)
				}
			})
		}
	}
}
func TestKnowledgeRefusalPreservesOrdinaryErrorBehavior(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			f := &refusalFixture{existing: existing, ordinaryFailure: true}
			err := runRefusalRestore(t, f, "selective")
			calls := refusalTrace(t, f, err)
			if err != nil {
				t.Fatal("ordinary API error propagation changed")
			}
			laterWrite := false
			for _, c := range calls {
				if c.Method == "POST" && c.Path == "/api/v1/models/import" {
					laterWrite = true
				}
			}
			if !laterWrite {
				t.Fatal("unrelated resource restore changed")
			}
		})
	}
}
func TestKnowledgeRefusalMalformedArchivePreflight(t *testing.T) {
	for name, body := range map[string]string{"null": "null", "array": "[]", "invalid": "{", "trailing": "{} {}", "bad-acl": `{"access_control":[]}`, "modern": `{"access_grants":null}`} {
		t.Run(name, func(t *testing.T) {
			path := refusalZip(t, t.TempDir(), "synthetic.zip", map[string]string{"knowledge_base.json": body})
			if err := RestoreKnowledge(nil, path, true); err == nil {
				t.Fatal("malformed/unsupported metadata reached client")
			}
		})
	}
}
