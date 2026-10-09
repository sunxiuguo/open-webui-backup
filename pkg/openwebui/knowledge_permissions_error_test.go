package openwebui

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type compatibilityReadFailure struct{}

func (compatibilityReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (compatibilityReadFailure) Close() error             { return nil }

func TestKnowledgeCompatibilityErrorClassification(t *testing.T) {
	transportErr := errors.New("synthetic transport failure")
	for _, tc := range []struct {
		name, body string
		status     int
		cause      error
	}{
		{"modern", `{"version":"0.11.4"}`, 200, nil},
		{"unavailable", "{}", 503, nil},
		{"malformed", "<html>", 200, nil},
		{"oversized", strings.Repeat(" ", 4097), 200, nil},
		{"transport", "", 0, transportErr},
		{"read", "", 200, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient("https://synthetic.invalid", "")
			client.httpClient.Transport = permissionTransport(func(*http.Request) (*http.Response, error) {
				if tc.cause == transportErr {
					return nil, transportErr
				}
				var body io.ReadCloser = io.NopCloser(strings.NewReader(tc.body))
				if tc.cause == io.ErrUnexpectedEOF {
					body = compatibilityReadFailure{}
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})
			err := client.CheckKnowledgePermissions()
			if !errors.Is(err, ErrKnowledgePermissionCompatibility) {
				t.Fatalf("compatibility failure is not identifiable: %v", err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("original cause was lost: %v", err)
			}
		})
	}
}
