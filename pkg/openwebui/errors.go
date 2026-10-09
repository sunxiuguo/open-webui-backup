package openwebui

import (
	"errors"
	"fmt"
)

// ErrKnowledgePermissionCompatibility identifies a refusal to use an
// unverified destination permission contract. Restore callers must stop.
var ErrKnowledgePermissionCompatibility = errors.New("knowledge permission compatibility refusal")

// APIError represents an error from the Open WebUI API
type APIError struct {
	StatusCode int
	Message    string
}

// Error implements the error interface for APIError
func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// FileExistsError indicates a backup file already exists
type FileExistsError struct {
	Path string
}

// Error implements the error interface for FileExistsError
func (e *FileExistsError) Error() string {
	return fmt.Sprintf("backup file already exists: %s", e.Path)
}

// KnowledgeExistsError indicates a knowledge base with the name already exists
type KnowledgeExistsError struct {
	Name string
}

// Error implements the error interface for KnowledgeExistsError
func (e *KnowledgeExistsError) Error() string {
	return fmt.Sprintf("knowledge base with name '%s' already exists", e.Name)
}
