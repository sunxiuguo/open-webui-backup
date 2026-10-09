package openwebui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgePermissionVersion(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		valid          bool
	}{
		{"verified legacy", `{"version":"0.6.18"}`, true},
		{"current modern", `{"version":"0.11.4"}`, false},
		{"earlier unverified", `{"version":"0.6.17"}`, false},
		{"later unverified", `{"version":"0.6.19"}`, false},
		{"custom build", `{"version":"0.6.18-custom"}`, false},
		{"tag prefix", `{"version":"v0.6.18"}`, false},
		{"empty", `{}`, false},
		{"null", `null`, false},
		{"wrong type", `{"version":618}`, false},
		{"invalid", `<html>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkKnowledgePermissionVersion([]byte(tc.response)); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}

func TestKnowledgeGrantsSurviveExportAndBlockRestore(t *testing.T) {
	for _, grants := range []string{`null`, `[]`, `[{"principal_type":"group","principal_id":"source-only","permission":"read"}]`, `{ "future": true }`} {
		for _, legacy := range []string{"", `,"access_control":{"read":{"group_ids":["legacy-only"]}}`} {
			input := `{"id":"synthetic","access_grants":` + grants + legacy + `}`
			var kb KnowledgeBase
			if err := json.Unmarshal([]byte(input), &kb); err != nil {
				t.Fatal(err)
			}
			output, err := json.Marshal(kb)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(output, &fields); err != nil {
				t.Fatal(err)
			}
			var before, after interface{}
			json.Unmarshal([]byte(grants), &before)
			json.Unmarshal(fields["access_grants"], &after)
			if fmt.Sprint(before) != fmt.Sprint(after) {
				t.Fatalf("grants lost: %s", output)
			}
			if kb.CheckRestorePermissions() == nil {
				t.Fatalf("accepted unsupported source: %s", input)
			}
		}
	}
	var legacy KnowledgeBase
	if err := json.Unmarshal([]byte(`{"access_control":{"read":{"group_ids":["original-id"]}}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := legacy.CheckRestorePermissions(); err != nil {
		t.Fatal(err)
	}
	output, _ := json.Marshal(legacy)
	if strings.Contains(string(output), "access_grants") || !strings.Contains(string(output), "original-id") {
		t.Fatalf("legacy changed: %s", output)
	}
}

type permissionTransport func(*http.Request) (*http.Response, error)

func (f permissionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKnowledgeMutationsRequireCompatibleVersion(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		for _, operation := range []string{"create", "update"} {
			t.Run(fmt.Sprintf("%s/%v", operation, compatible), func(t *testing.T) {
				mutations := 0
				c := NewClient("https://synthetic.invalid", "synthetic-key")
				c.httpClient.Transport = permissionTransport(func(r *http.Request) (*http.Response, error) {
					body := `{ "id":"synthetic" }`
					if r.Method == "GET" && r.URL.Path == "/api/version" {
						body = `{"version":"0.11.4"}`
						if compatible {
							body = `{"version":"0.6.18"}`
						}
					} else {
						mutations++
						bytes, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(bytes), "original-id") {
							t.Errorf("legacy ACL not transmitted: %s", bytes)
						}
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})
				form := &KnowledgeForm{AccessControl: map[string]interface{}{"read": map[string]interface{}{"group_ids": []string{"original-id"}}}}
				var err error
				if operation == "create" {
					_, err = c.CreateKnowledge(form)
				} else {
					err = c.UpdateKnowledge("synthetic", form)
				}
				if (err == nil) != compatible {
					t.Fatalf("compatible=%v err=%v", compatible, err)
				}
				expected := 0
				if compatible {
					expected = 1
				}
				if mutations != expected {
					t.Fatalf("mutations=%d expected=%d", mutations, expected)
				}
			})
		}
	}
}

func TestUnavailableVersionBlocksMutation(t *testing.T) {
	c := NewClient("https://synthetic.invalid", "")
	c.httpClient.Transport = permissionTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("mutation sent without verified version")
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})
	if _, err := c.CreateKnowledge(&KnowledgeForm{}); err == nil {
		t.Fatal("404 version endpoint accepted")
	}
}
