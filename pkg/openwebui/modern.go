package openwebui

import (
	"bytes"
	"encoding/json"
	"io"
)

// PostJSON is used by versioned workflows with explicit modern request contracts.
func (c *Client) PostJSON(path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest("POST", path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: string(raw)}
	}
	return raw, nil
}
