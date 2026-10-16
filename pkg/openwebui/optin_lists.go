package openwebui

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func (c *Client) ListRepresentationFiles() ([]FileMetadata, error) {
	var files []FileMetadata
	seen := map[string]bool{}
	expected := -1
	for page := 1; ; page++ {
		raw, err := c.ReadFileEndpoint(fmt.Sprintf("/api/v1/files/?page=%d", page))
		if err != nil {
			return nil, err
		}
		var batch []FileMetadata
		// Older servers returned a single unpaginated array.
		if len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '[' {
			if page != 1 {
				return nil, fmt.Errorf("file list changed response format")
			}
			if err := json.Unmarshal(raw, &batch); err != nil {
				return nil, err
			}
			expected = len(batch)
		} else {
			var response struct {
				Items *[]FileMetadata `json:"items"`
				Total *int            `json:"total"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				return nil, err
			}
			if response.Items == nil || response.Total == nil || *response.Total < 0 {
				return nil, fmt.Errorf("invalid file list response")
			}
			if expected >= 0 && expected != *response.Total {
				return nil, fmt.Errorf("file list changed during pagination")
			}
			expected = *response.Total
			batch = *response.Items
		}
		for _, file := range batch {
			if file.ID == "" || seen[file.ID] {
				return nil, fmt.Errorf("empty or duplicate file ID in pagination")
			}
			seen[file.ID] = true
			files = append(files, file)
		}
		if len(files) == expected {
			return files, nil
		}
		if len(files) > expected || len(batch) == 0 {
			return nil, fmt.Errorf("incomplete file pagination")
		}
	}
}

// GetFileWithContent fetches a file with its full content
func (c *Client) ListModernKnowledge() ([]KnowledgeBase, error) {
	result := []KnowledgeBase{}
	seen := map[string]bool{}
	total := -1
	for page := 1; ; page++ {
		raw, err := c.ReadFileEndpoint(fmt.Sprintf("/api/v1/knowledge/?page=%d", page))
		if err != nil {
			return nil, err
		}
		var p struct {
			Items *[]KnowledgeBase `json:"items"`
			Total *int             `json:"total"`
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Items == nil || p.Total == nil || *p.Total < 0 {
			return nil, fmt.Errorf("invalid knowledge page")
		}
		if total >= 0 && total != *p.Total {
			return nil, fmt.Errorf("knowledge pagination changed")
		}
		total = *p.Total
		for _, k := range *p.Items {
			if k.ID == "" || seen[k.ID] {
				return nil, fmt.Errorf("invalid or duplicate knowledge ID")
			}
			seen[k.ID] = true
			result = append(result, k)
		}
		if len(result) == total {
			return result, nil
		}
		if len(result) > total || len(*p.Items) == 0 {
			return nil, fmt.Errorf("incomplete knowledge pagination")
		}
	}
}

// GetFile fetches a single file by ID
