package openwebui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

// ReadFileEndpoint reads an API response, preserving arbitrary metadata and binary bytes.
func (c *Client) ReadFileEndpoint(path string) ([]byte, error) {
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	return body, nil
}
func (c *Client) OriginalFile(id string) ([]byte, error) {
	return c.ReadFileEndpoint("/api/v1/files/" + url.PathEscape(id) + "/content")
}
func (c *Client) IndexedFile(id string) (string, error) {
	raw, err := c.ReadFileEndpoint("/api/v1/files/" + url.PathEscape(id) + "/data/content")
	if err != nil {
		return "", err
	}
	var response struct {
		Content *string `json:"content"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	if response.Content == nil {
		return "", fmt.Errorf("indexed content is missing or null")
	}
	return *response.Content, nil
}
func (c *Client) UpdateIndexedFile(id, content string) error {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	resp, err := c.doRequest("POST", "/api/v1/files/"+url.PathEscape(id)+"/data/content/update", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Message: string(b)}
	}
	return nil
}
