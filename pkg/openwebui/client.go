package openwebui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// Client represents an HTTP client for the Open WebUI API
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new API client instance
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// GetBaseURL returns the base URL of the client
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// ListKnowledge fetches all knowledge bases from the API
func (c *Client) ListKnowledge() ([]KnowledgeBase, error) {
	resp, err := c.doRequest("GET", "/api/v1/knowledge/list", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var knowledgeBases []KnowledgeBase
	if err := json.NewDecoder(resp.Body).Decode(&knowledgeBases); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return knowledgeBases, nil
}

// GetFile fetches a single file by ID
func (c *Client) GetFile(fileID string) (*FileData, error) {
	path := fmt.Sprintf("/api/v1/files/%s", fileID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var fileData FileData
	if err := json.NewDecoder(resp.Body).Decode(&fileData); err != nil {
		return nil, fmt.Errorf("failed to decode file response: %w", err)
	}

	return &fileData, nil
}

// CreateKnowledge creates a new knowledge base
func (c *Client) CreateKnowledge(form *KnowledgeForm) (*KnowledgeCreateResponse, error) {
	if err := c.CheckKnowledgePermissions(); err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(form)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal knowledge form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/knowledge/create", bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var result KnowledgeCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// UploadFile uploads a file via multipart/form-data
func (c *Client) UploadFile(filename string, content []byte) (*FileUploadResponse, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Create form file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("failed to write file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Make the request with process=true and process_in_background=false
	// This ensures the file is fully processed before we try to link it
	url := c.baseURL + "/api/v1/files/?process=true&process_in_background=false"
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var result FileUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetKnowledgeByID fetches a specific knowledge base with its files
func (c *Client) GetKnowledgeByID(id string) (*KnowledgeBase, error) {
	path := fmt.Sprintf("/api/v1/knowledge/%s", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var kb KnowledgeBase
	if err := json.NewDecoder(resp.Body).Decode(&kb); err != nil {
		return nil, fmt.Errorf("failed to decode knowledge base response: %w", err)
	}

	return &kb, nil
}

// UpdateKnowledge updates an existing knowledge base
func (c *Client) UpdateKnowledge(id string, form *KnowledgeForm) error {
	if err := c.CheckKnowledgePermissions(); err != nil {
		return err
	}
	jsonData, err := json.Marshal(form)
	if err != nil {
		return fmt.Errorf("failed to marshal knowledge form: %w", err)
	}

	path := fmt.Sprintf("/api/v1/knowledge/%s/update", id)
	resp, err := c.doRequest("POST", path, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// AddFileToKnowledge associates an uploaded file with a knowledge base
func (c *Client) AddFileToKnowledge(knowledgeID, fileID string) error {
	reqBody := AddFileRequest{FileID: fileID}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	path := fmt.Sprintf("/api/v1/knowledge/%s/file/add", knowledgeID)
	resp, err := c.doRequest("POST", path, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// RemoveFileFromKnowledge removes a file from a knowledge base
func (c *Client) RemoveFileFromKnowledge(knowledgeID, fileID string) error {
	reqBody := AddFileRequest{FileID: fileID}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	path := fmt.Sprintf("/api/v1/knowledge/%s/file/remove", knowledgeID)
	resp, err := c.doRequest("POST", path, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ExportModels fetches all models via /api/v1/models/export
func (c *Client) ExportModels() ([]Model, error) {
	resp, err := c.doRequest("GET", "/api/v1/models/export", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var models []Model
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	return models, nil
}

// ImportModels posts models to /api/v1/models/import
func (c *Client) ImportModels(models []Model) error {
	reqBody := map[string]interface{}{
		"models": models,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal models: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/models/import", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// GetModelByID fetches a specific model by ID
func (c *Client) GetModelByID(id string) (*Model, error) {
	path := fmt.Sprintf("/api/v1/models/?id=%s", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	// API returns array, we need first element
	var models []Model
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to decode model response: %w", err)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("model not found: %s", id)
	}

	return &models[0], nil
}

// ExportTools fetches all tools from /api/v1/tools/export
func (c *Client) ExportTools() ([]Tool, error) {
	resp, err := c.doRequest("GET", "/api/v1/tools/export", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var tools []Tool
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("failed to decode tools response: %w", err)
	}

	return tools, nil
}

// ImportTool creates or imports a tool
func (c *Client) ImportTool(tool *ToolForm) error {
	jsonData, err := json.Marshal(tool)
	if err != nil {
		return fmt.Errorf("failed to marshal tool form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/tools/create", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ListPrompts fetches all prompts from /api/v1/prompts/
func (c *Client) ListPrompts() ([]Prompt, error) {
	resp, err := c.doRequest("GET", "/api/v1/prompts/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var prompts []Prompt
	if err := json.NewDecoder(resp.Body).Decode(&prompts); err != nil {
		return nil, fmt.Errorf("failed to decode prompts response: %w", err)
	}

	return prompts, nil
}

// CreatePrompt creates a new prompt
func (c *Client) CreatePrompt(prompt *PromptForm) error {
	jsonData, err := json.Marshal(prompt)
	if err != nil {
		return fmt.Errorf("failed to marshal prompt form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/prompts/create", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ListFiles fetches all files (metadata only) from /api/v1/files/
func (c *Client) ListFiles() ([]FileMetadata, error) {
	resp, err := c.doRequest("GET", "/api/v1/files/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var files []FileMetadata
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode files response: %w", err)
	}

	return files, nil
}

// GetFileWithContent fetches a file with its full content
func (c *Client) GetFileWithContent(id string) (*FileExport, error) {
	path := fmt.Sprintf("/api/v1/files/%s?content=true", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var fileExport FileExport
	if err := json.NewDecoder(resp.Body).Decode(&fileExport); err != nil {
		return nil, fmt.Errorf("failed to decode file export response: %w", err)
	}

	return &fileExport, nil
}

// CreateFileFromExport uploads a file from export data
func (c *Client) CreateFileFromExport(file *FileExport) error {
	// Use the existing UploadFile method to upload the file content
	var content []byte
	if file.Data != nil && file.Data.Content != "" {
		content = []byte(file.Data.Content)
	}

	_, err := c.UploadFile(file.Filename, content)
	return err
}

// GetAllChats fetches all chats from /api/v1/chats/all
func (c *Client) GetAllChats() ([]Chat, error) {
	resp, err := c.doRequest("GET", "/api/v1/chats/all", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []Chat
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode chats response: %w", err)
	}

	return chats, nil
}

// GetChatsList fetches paginated list of chats from /api/v1/chats/list
func (c *Client) GetChatsList(page int, includePinned, includeFolders bool) ([]ChatTitleID, error) {
	path := fmt.Sprintf("/api/v1/chats/list?page=%d&include_pinned=%t&include_folders=%t",
		page, includePinned, includeFolders)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []ChatTitleID
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode chats list response: %w", err)
	}

	return chats, nil
}

// GetAllChatsDB fetches all chats from database /api/v1/chats/all/db
func (c *Client) GetAllChatsDB() ([]Chat, error) {
	resp, err := c.doRequest("GET", "/api/v1/chats/all/db", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []Chat
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode chats db response: %w", err)
	}

	return chats, nil
}

// SearchChats searches chats by text query
func (c *Client) SearchChats(query string, page int) ([]ChatTitleID, error) {
	path := fmt.Sprintf("/api/v1/chats/search?text=%s&page=%d", query, page)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []ChatTitleID
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %w", err)
	}

	return chats, nil
}

// GetChatsByFolder fetches chats in a specific folder
func (c *Client) GetChatsByFolder(folderID string, page int) ([]Chat, error) {
	path := fmt.Sprintf("/api/v1/chats/folder/%s/list?page=%d", folderID, page)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []Chat
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode folder chats: %w", err)
	}

	return chats, nil
}

// GetArchivedChats fetches archived chats
func (c *Client) GetArchivedChats(page int, query, orderBy, direction string) ([]ChatTitleID, error) {
	path := fmt.Sprintf("/api/v1/chats/archived?page=%d", page)
	if query != "" {
		path += fmt.Sprintf("&query=%s", query)
	}
	if orderBy != "" {
		path += fmt.Sprintf("&order_by=%s", orderBy)
	}
	if direction != "" {
		path += fmt.Sprintf("&direction=%s", direction)
	}

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chats []ChatTitleID
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return nil, fmt.Errorf("failed to decode archived chats: %w", err)
	}

	return chats, nil
}

// GetSharedChat fetches a shared chat by share ID
func (c *Client) GetSharedChat(shareID string) (*Chat, error) {
	path := fmt.Sprintf("/api/v1/chats/share/%s", shareID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chat Chat
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return nil, fmt.Errorf("failed to decode shared chat: %w", err)
	}

	return &chat, nil
}

// GetChatByID fetches a specific chat by ID
func (c *Client) GetChatByID(id string) (*Chat, error) {
	path := fmt.Sprintf("/api/v1/chats/%s", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var chat Chat
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return nil, fmt.Errorf("failed to decode chat response: %w", err)
	}

	return &chat, nil
}

// ImportChat imports a chat via /api/v1/chats/import
func (c *Client) ImportChat(chat *Chat) error {
	jsonData, err := json.Marshal(chat)
	if err != nil {
		return fmt.Errorf("failed to marshal chat: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/chats/import", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllChats deletes all user chats
func (c *Client) DeleteAllChats() error {
	resp, err := c.doRequest("DELETE", "/api/v1/chats/", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteChatByID deletes a specific chat by ID
func (c *Client) DeleteChatByID(id string) error {
	path := fmt.Sprintf("/api/v1/chats/%s", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllFiles deletes all files
func (c *Client) DeleteAllFiles() error {
	resp, err := c.doRequest("DELETE", "/api/v1/files/all", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllModels deletes all models
func (c *Client) DeleteAllModels() error {
	resp, err := c.doRequest("DELETE", "/api/v1/models/delete/all", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteKnowledgeByID deletes a specific knowledge base by ID
func (c *Client) DeleteKnowledgeByID(id string) error {
	path := fmt.Sprintf("/api/v1/knowledge/%s/delete", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeletePromptByCommand deletes a specific prompt by command
func (c *Client) DeletePromptByCommand(command string) error {
	if command != "" && strings.HasPrefix(command, "/") {
		command = command[1:]
	}

	path := fmt.Sprintf("/api/v1/prompts/command/%s/delete", command)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ListTools fetches all tools from /api/v1/tools/
func (c *Client) ListTools() ([]Tool, error) {
	resp, err := c.doRequest("GET", "/api/v1/tools/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var tools []Tool
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil, fmt.Errorf("failed to decode tools response: %w", err)
	}

	return tools, nil
}

// DeleteToolByID deletes a specific tool by ID
func (c *Client) DeleteToolByID(id string) error {
	path := fmt.Sprintf("/api/v1/tools/id/%s/delete", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ListFunctions fetches all functions from /api/v1/functions/export
func (c *Client) ListFunctions() ([]Function, error) {
	resp, err := c.doRequest("GET", "/api/v1/functions/export", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var functions []Function
	if err := json.NewDecoder(resp.Body).Decode(&functions); err != nil {
		return nil, fmt.Errorf("failed to decode functions response: %w", err)
	}

	return functions, nil
}

// DeleteFunctionByID deletes a specific function by ID
func (c *Client) DeleteFunctionByID(id string) error {
	path := fmt.Sprintf("/api/v1/functions/id/%s/delete", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// ListMemories fetches all memories from /api/v1/memories/
func (c *Client) ListMemories() ([]Memory, error) {
	resp, err := c.doRequest("GET", "/api/v1/memories/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var memories []Memory
	if err := json.NewDecoder(resp.Body).Decode(&memories); err != nil {
		return nil, fmt.Errorf("failed to decode memories response: %w", err)
	}

	return memories, nil
}

// DeleteMemoryByID deletes a specific memory by ID
func (c *Client) DeleteMemoryByID(id string) error {
	path := fmt.Sprintf("/api/v1/memories/%s", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllMemories deletes all user memories
func (c *Client) DeleteAllMemories() error {
	resp, err := c.doRequest("DELETE", "/api/v1/memories/delete/user", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllFeedbacks deletes all feedbacks
func (c *Client) DeleteAllFeedbacks() error {
	resp, err := c.doRequest("DELETE", "/api/v1/evaluations/feedbacks/all", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// GetAllUsers fetches all users from /api/v1/users/ with pagination support
func (c *Client) GetAllUsers() ([]User, error) {
	var allUsers []User
	page := 1

	for {
		path := fmt.Sprintf("/api/v1/users/?page=%d", page)
		resp, err := c.doRequest("GET", path, nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    string(body),
			}
		}

		var response UserListResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode users response: %w", err)
		}
		resp.Body.Close()

		// If no users returned, we've reached the end
		if len(response.Users) == 0 {
			break
		}

		allUsers = append(allUsers, response.Users...)

		// Check if we've fetched all users
		if len(allUsers) >= response.Total {
			break
		}

		page++
	}

	return allUsers, nil
}

// ImportUser creates a new user via /api/v1/auths/add
func (c *Client) ImportUser(userForm *UserForm) error {
	jsonData, err := json.Marshal(userForm)
	if err != nil {
		return fmt.Errorf("failed to marshal user form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/auths/add", bytes.NewReader(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteUserByID deletes a specific user by ID
func (c *Client) DeleteUserByID(userID string) error {
	path := fmt.Sprintf("/api/v1/users/%s", userID)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// GetAPIKey returns the API key being used by the client
func (c *Client) GetAPIKey() string {
	return c.apiKey
}

// GetAllGroups fetches all groups from /api/v1/groups/
func (c *Client) GetAllGroups() ([]Group, error) {
	resp, err := c.doRequest("GET", "/api/v1/groups/", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var groups []Group
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return nil, fmt.Errorf("failed to decode groups response: %w", err)
	}

	return groups, nil
}

// CreateGroup creates a new group via /api/v1/groups/create
func (c *Client) CreateGroup(groupForm *GroupForm) (*Group, error) {
	jsonData, err := json.Marshal(groupForm)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal group form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/groups/create", bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var group Group
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, fmt.Errorf("failed to decode group response: %w", err)
	}

	return &group, nil
}

// GetGroupByID fetches a specific group by ID
func (c *Client) GetGroupByID(id string) (*Group, error) {
	path := fmt.Sprintf("/api/v1/groups/id/%s", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var group Group
	if err := json.NewDecoder(resp.Body).Decode(&group); err != nil {
		return nil, fmt.Errorf("failed to decode group response: %w", err)
	}

	return &group, nil
}

// DeleteGroupByID deletes a specific group by ID
func (c *Client) DeleteGroupByID(id string) error {
	path := fmt.Sprintf("/api/v1/groups/id/%s/delete", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// DeleteAllGroups deletes all groups
func (c *Client) DeleteAllGroups() error {
	resp, err := c.doRequest("DELETE", "/api/v1/groups/all", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// GetAllFeedbacks fetches all feedbacks from /api/v1/evaluations/feedbacks/all/export
func (c *Client) GetAllFeedbacks() ([]Feedback, error) {
	resp, err := c.doRequest("GET", "/api/v1/evaluations/feedbacks/all/export", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var feedbacks []Feedback
	if err := json.NewDecoder(resp.Body).Decode(&feedbacks); err != nil {
		return nil, fmt.Errorf("failed to decode feedbacks response: %w", err)
	}

	return feedbacks, nil
}

// CreateFeedback creates a new feedback
func (c *Client) CreateFeedback(feedbackForm *FeedbackForm) (*Feedback, error) {
	jsonData, err := json.Marshal(feedbackForm)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal feedback form: %w", err)
	}

	resp, err := c.doRequest("POST", "/api/v1/evaluations/feedback", bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var feedback Feedback
	if err := json.NewDecoder(resp.Body).Decode(&feedback); err != nil {
		return nil, fmt.Errorf("failed to decode feedback response: %w", err)
	}

	return &feedback, nil
}

// GetFeedbackByID fetches a specific feedback by ID
func (c *Client) GetFeedbackByID(id string) (*Feedback, error) {
	path := fmt.Sprintf("/api/v1/evaluations/feedback/%s", id)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var feedback Feedback
	if err := json.NewDecoder(resp.Body).Decode(&feedback); err != nil {
		return nil, fmt.Errorf("failed to decode feedback response: %w", err)
	}

	return &feedback, nil
}

// DeleteFeedbackByID deletes a specific feedback by ID
func (c *Client) DeleteFeedbackByID(id string) error {
	path := fmt.Sprintf("/api/v1/evaluations/feedback/%s", id)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// doRequest makes an authenticated HTTP request to the API
func (c *Client) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication header
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}
