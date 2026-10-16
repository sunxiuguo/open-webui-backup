package knowledgearchive_test

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/vosiander/open-webui-backup/pkg/backup"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"github.com/vosiander/open-webui-backup/pkg/restore"
)

/*
Fixed test fixture copied from filippo.io/age v1.2.1 testdata/example_keys.txt.
Original labels it "Test key for ExampleParseIdentities." This is publicly
published synthetic test data, not a generated or real account credential.
Source SHA256: aae4728b2967a9f8aeba742beaac8f7f55760392eed0164be641bffa3fac9de9

# Copyright 2019 The age Authors

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

  - Redistributions of source code must retain the above copyright

notice, this list of conditions and the following disclaimer.
  - Redistributions in binary form must reproduce the above

copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
  - Neither the name of the age project nor the names of its

contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/
const fixtureIdentity = "AGE-SECRET-KEY-184JMZMVQH3E6U0PSL869004Y3U2NYV7R30EU99CSEDNPH02YUVFSZW44VU"

func TestCompiledModernCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "owuicli")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/owuicli")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	id, e := age.ParseX25519Identity(fixtureIdentity)
	if e != nil {
		t.Fatal(e)
	}
	identity := filepath.Join(dir, "synthetic-age.txt")
	os.WriteFile(identity, []byte(fixtureIdentity+"\n"), 0600)
	run := func(url string, args ...string) (string, error) {
		c := exec.Command(bin, args...)
		c.Env = append(os.Environ(), "OPEN_WEBUI_URL="+url, "OPEN_WEBUI_API_KEY=synthetic", "POSTGRES_URL=", "OWUI_ENCRYPTED_RECIPIENT=", "OWUI_DECRYPT_IDENTITY=")
		out, e := c.CombinedOutput()
		return string(out), e
	}
	if out, e := run("http://127.0.0.1:1", "modern-knowledge", "--help"); e != nil || !strings.Contains(out, "0.12.2") || !strings.Contains(out, "plaintext") {
		t.Fatalf("help: %v %s", e, out)
	}
	if _, e := run("http://127.0.0.1:1", "modern-knowledge", "backup", "--out", filepath.Join(dir, "missing-key")); e == nil {
		t.Fatal("missing recipient succeeded")
	}
	src := newService(t, true)
	server := src.server()
	defer server.Close()
	archive := filepath.Join(dir, "capture.age")
	if out, e := run(server.URL, "modern-knowledge", "backup", "--out", archive, "--encrypt-recipient", id.Recipient().String()); e != nil {
		t.Fatalf("capture %v %s", e, out)
	}
	raw, _ := os.ReadFile(archive)
	if !strings.HasPrefix(string(raw), "-----BEGIN AGE ENCRYPTED FILE-----") {
		t.Fatal("plaintext output")
	}
	stat, _ := os.Stat(archive)
	if stat.Mode().Perm() != 0600 {
		t.Fatal("output permissions")
	}
	if tmp, _ := filepath.Glob(filepath.Join(dir, ".modern-kb-*")); len(tmp) != 0 {
		t.Fatal("plaintext temp remains")
	}
	if _, e := run(server.URL, "modern-knowledge", "backup", "--out", archive, "--encrypt-recipient", id.Recipient().String()); e == nil {
		t.Fatal("overwrite accepted")
	}
	for _, mode := range []string{"success", "link-noop"} {
		t.Run(mode, func(t *testing.T) {
			dst := newService(t, false)
			if mode != "success" {
				dst.mode = mode
			}
			reports := t.TempDir()
			dst.reportDir = reports
			s := dst.server()
			defer s.Close()
			out, e := run(s.URL, "modern-knowledge", "restore", "--file", archive, "--decrypt-identity", identity, "--report-dir", reports)
			if (e == nil) != (mode == "success") {
				t.Fatalf("exit %v %s", e, out)
			}
			paths, _ := filepath.Glob(filepath.Join(reports, "knowledge-restore-*.json"))
			if len(paths) != 1 {
				t.Fatalf("report absent %s", out)
			}
			if !strings.Contains(out, paths[0]) {
				t.Fatalf("report not exposed %s", out)
			}
			raw, _ := os.ReadFile(paths[0])
			var result struct {
				Status string            `json:"status"`
				Files  map[string]string `json:"file_id_map"`
			}
			if e = json.Unmarshal(raw, &result); e != nil {
				t.Fatal(e)
			}
			expected := "complete"
			if mode != "success" {
				expected = "incomplete"
			}
			if result.Status != expected || len(result.Files) != 2 {
				t.Fatalf("result %s", raw)
			}
			st, _ := os.Stat(paths[0])
			if st.Mode().Perm() != 0600 {
				t.Fatal("report permissions")
			}
		})
	}
	// Ordinary CLI must reject the encrypted modern bundle after decryption, with no target HTTP.
	dst := newService(t, false)
	s := dst.server()
	defer s.Close()
	if out, e := run(s.URL, "restore", "--file", archive, "--decrypt-identity", identity); e == nil || len(dst.requests) != 0 {
		t.Fatalf("default downgrade %v %s %v", e, out, dst.requests)
	}
}

func TestLegacyDefaultEndpointsAndFormats(t *testing.T) {
	calls := []string{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/knowledge/list":
			io.WriteString(w, `[]`)
		case "/api/v1/files/":
			io.WriteString(w, `[]`)
		case "/api/v1/models/export", "/api/v1/tools/export", "/api/v1/prompts/", "/api/v1/chats/all/db", "/api/v1/groups/", "/api/v1/evaluations/feedbacks/all/export":
			io.WriteString(w, `[]`)
		case "/api/v1/users/":
			io.WriteString(w, `{"users":[],"total":0}`)
		default:
			t.Errorf("unexpected modern/other route %s", r.URL)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer s.Close()
	c := openwebui.NewClient(s.URL, "synthetic")
	if _, e := c.ListKnowledge(); e != nil {
		t.Fatal(e)
	}
	if _, e := c.ListFiles(); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e := backup.BackupAll(c, dir); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "mixed.zip")
	if e := backup.BackupSelective(c, path, &backup.SelectiveBackupOptions{Knowledge: true, Models: true, Files: true}, nil); e != nil {
		t.Fatal(e)
	}
	r, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "knowledge-manifest.json" {
			t.Fatal("default modern format")
		}
	}
	for _, call := range calls {
		if strings.Contains(call, "/api/version") || strings.Contains(call, "view_option") || strings.Contains(call, "/files/?") {
			t.Fatal(call)
		}
	}
}
func TestAllOrdinaryRestoresRejectModernDeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken-modern.zip")
	f, _ := os.Create(path)
	w := zip.NewWriter(f)
	e, _ := w.Create("owui.json")
	fmt.Fprint(e, `{"format":"modern-knowledge-v1","unified_backup":true,"contained_types":["model"]}`)
	w.Close()
	f.Close()
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++ }))
	defer s.Close()
	c := openwebui.NewClient(s.URL, "synthetic")
	for _, fn := range []func(*openwebui.Client, string, bool) error{restore.RestoreKnowledge, restore.RestoreModel, restore.RestoreUser, restore.RestoreFile, restore.RestoreTool, restore.RestorePrompt, restore.RestoreChat, restore.RestoreGroup, restore.RestoreFeedback} {
		if e := fn(c, path, false); e == nil {
			t.Fatal("modern accepted")
		}
	}
	if e := restore.RestoreSelective(c, path, &restore.SelectiveRestoreOptions{Models: true}, false, nil); e == nil {
		t.Fatal("selective accepted")
	}
	if e := restore.RestoreAll(c, filepath.Dir(path), false); e == nil {
		t.Fatal("all accepted")
	}
	if n != 0 {
		t.Fatal("network before rejection")
	}
}
