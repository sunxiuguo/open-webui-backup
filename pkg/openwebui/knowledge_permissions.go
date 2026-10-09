package openwebui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CheckKnowledgePermissions uses the read-only version endpoint, which is
// available in production (unlike OpenAPI). Only exact, reviewed official
// contracts are allowed. Never infer compatibility from a version range.
func (c *Client) CheckKnowledgePermissions() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrKnowledgePermissionCompatibility, err)
		}
	}()
	resp, err := c.doRequest(http.MethodGet, "/api/version", nil)
	if err != nil {
		return fmt.Errorf("cannot verify knowledge permission compatibility: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot verify knowledge permission compatibility: /api/version returned HTTP %d", resp.StatusCode)
	}
	const maxVersionSize = 4096
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVersionSize+1))
	if err != nil {
		return fmt.Errorf("cannot read destination version: %w", err)
	}
	if len(data) > maxVersionSize {
		return fmt.Errorf("destination version response exceeds %d bytes", maxVersionSize)
	}
	return checkKnowledgePermissionVersion(data)
}

func checkKnowledgePermissionVersion(data []byte) error {
	var result struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("invalid destination version: %w", err)
	}
	switch result.Version {
	case "0.6.18":
		// Official v0.6.18, commit 5fbfe2bdcadf5f157926f6551891e4dc0802b9f3:
		// KnowledgeForm has access_control: Optional[dict]; /create and /{id}/update
		// both consume it. See docs/knowledge-permission-contracts.md before adding
		// another exact version. This is not a general endorsement of old releases.
		return nil
	default:
		return fmt.Errorf("unsupported knowledge permission contract for destination version %q: only official 0.6.18 is verified for legacy access_control; restore stopped to avoid losing permissions", result.Version)
	}
}

// CheckRestorePermissions does not translate user/group identifiers. Even null
// or empty access_grants identifies a newer (or mixed) archive contract.
func (kb *KnowledgeBase) CheckRestorePermissions() error {
	if len(kb.AccessGrants) != 0 {
		return fmt.Errorf("knowledge %q uses access_grants: permission migration and identity mapping are not supported; restore stopped", kb.ID)
	}
	return nil
}
