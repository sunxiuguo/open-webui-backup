package openwebui

import "encoding/json"

// KnowledgeBase represents a knowledge base from the API
type KnowledgeBase struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Data          *KnowledgeData         `json:"data"`
	Meta          map[string]interface{} `json:"meta"`
	AccessGrants  json.RawMessage        `json:"access_grants,omitempty"`
	AccessControl map[string]interface{} `json:"access_control"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
	Files         []FileMetadata         `json:"files"`
}

// KnowledgeData contains file IDs associated with the knowledge base
type KnowledgeData struct {
	FileIDs []string `json:"file_ids"`
}

// FileMetadata represents file metadata from the API
type FileMetadata struct {
	ID        string   `json:"id"`
	Meta      FileMeta `json:"meta"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

// FileMeta contains file-specific metadata
type FileMeta struct {
	Name           string                 `json:"name"`
	ContentType    string                 `json:"content_type"`
	Size           int64                  `json:"size"`
	Data           map[string]interface{} `json:"data"`
	CollectionName string                 `json:"collection_name"`
}

// FileData represents a complete file with content from the API
type FileData struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Hash          string                 `json:"hash"`
	Filename      string                 `json:"filename"`
	Path          string                 `json:"path"`
	Data          *FileContent           `json:"data"`
	Meta          FileMeta               `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
}

// FileContent contains the actual file content
type FileContent struct {
	Status  string `json:"status"`
	Content string `json:"content"`
}

// KnowledgeForm represents the request body for creating/updating knowledge
type KnowledgeForm struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Data          *KnowledgeData         `json:"data,omitempty"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
}

// KnowledgeCreateResponse represents the response from creating a knowledge base
type KnowledgeCreateResponse struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Data          *KnowledgeData         `json:"data"`
	Meta          map[string]interface{} `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
}

// FileUploadResponse represents the response from uploading a file
type FileUploadResponse struct {
	ID        string   `json:"id"`
	UserID    string   `json:"user_id"`
	Filename  string   `json:"filename"`
	Meta      FileMeta `json:"meta"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

// AddFileRequest represents the request to add a file to knowledge base
type AddFileRequest struct {
	FileID string `json:"file_id"`
}

// Model represents a model from the Open WebUI API
type Model struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	BaseModelID   *string                `json:"base_model_id"`
	Name          string                 `json:"name"`
	Params        map[string]interface{} `json:"params"`
	Meta          ModelMeta              `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control"`
	IsActive      bool                   `json:"is_active"`
	UpdatedAt     int64                  `json:"updated_at"`
	CreatedAt     int64                  `json:"created_at"`
}

// ModelMeta contains model metadata including knowledge base references
type ModelMeta struct {
	ProfileImageURL string                   `json:"profile_image_url,omitempty"`
	Description     *string                  `json:"description,omitempty"`
	Capabilities    map[string]interface{}   `json:"capabilities,omitempty"`
	Knowledge       []map[string]interface{} `json:"knowledge,omitempty"` // Can be files or collections
	Tags            interface{}              `json:"tags,omitempty"`      // Flexible type to handle API changes
}

// KnowledgeItem represents a single knowledge item (file or collection)
type KnowledgeItem struct {
	Type string                 `json:"type"` // "file" or "collection"
	Data map[string]interface{} `json:"-"`    // Raw data for flexibility
}

// ModelForm for creating/updating models
type ModelForm struct {
	ID            string                 `json:"id"`
	BaseModelID   *string                `json:"base_model_id,omitempty"`
	Name          string                 `json:"name"`
	Params        map[string]interface{} `json:"params"`
	Meta          ModelMeta              `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
	IsActive      bool                   `json:"is_active"`
}

// Tool represents a tool from the Open WebUI API
type Tool struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Name          string                 `json:"name"`
	Content       string                 `json:"content"`
	Meta          ToolMeta               `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control"`
	UpdatedAt     int64                  `json:"updated_at"`
	CreatedAt     int64                  `json:"created_at"`
}

// ToolMeta contains tool-specific metadata
type ToolMeta struct {
	Description string                 `json:"description,omitempty"`
	Manifest    map[string]interface{} `json:"manifest,omitempty"`
}

// ToolForm for creating/updating tools
type ToolForm struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Content       string                 `json:"content"`
	Meta          ToolMeta               `json:"meta"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
}

// Prompt represents a prompt from the Open WebUI API
type Prompt struct {
	Command       string                 `json:"command"`
	Title         string                 `json:"title"`
	Content       string                 `json:"content"`
	UserID        string                 `json:"user_id,omitempty"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
	UpdatedAt     int64                  `json:"updated_at,omitempty"`
	CreatedAt     int64                  `json:"created_at,omitempty"`
}

// PromptForm for creating/updating prompts
type PromptForm struct {
	Command       string                 `json:"command"`
	Title         string                 `json:"title"`
	Content       string                 `json:"content"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
}

// FileExport represents a complete file export with all metadata
type FileExport struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Filename      string                 `json:"filename"`
	Meta          FileMeta               `json:"meta"`
	Data          *FileContent           `json:"data"`
	Hash          string                 `json:"hash,omitempty"`
	AccessControl map[string]interface{} `json:"access_control,omitempty"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
}

// Chat represents a chat conversation from the Open WebUI API
type Chat struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Title     string                 `json:"title"`
	Chat      ChatMessages           `json:"chat"` // Array of messages
	Meta      map[string]interface{} `json:"meta,omitempty"`
	CreatedAt int64                  `json:"created_at"`
	UpdatedAt int64                  `json:"updated_at"`
}

// ChatTitleID represents a simplified chat response from list/search endpoints
type ChatTitleID struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UserID    string `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// ChatMessages represents the messages array in a chat
type ChatMessages struct {
	Messages []Message `json:"messages"`
}

// Message represents a single message in a chat
type Message struct {
	ID          string                 `json:"id,omitempty"`
	ParentID    *string                `json:"parentId,omitempty"`
	ChildrenIDs []string               `json:"childrenIds,omitempty"`
	Role        string                 `json:"role"`
	Content     string                 `json:"content"`
	Model       string                 `json:"model,omitempty"`
	Timestamp   int64                  `json:"timestamp,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

// Function represents a function from the Open WebUI API
type Function struct {
	ID        string       `json:"id"`
	UserID    string       `json:"user_id"`
	Name      string       `json:"name"`
	Type      string       `json:"type"`
	Content   string       `json:"content"`
	Meta      FunctionMeta `json:"meta"`
	IsActive  bool         `json:"is_active"`
	IsGlobal  bool         `json:"is_global"`
	UpdatedAt int64        `json:"updated_at"`
	CreatedAt int64        `json:"created_at"`
}

// FunctionMeta contains function-specific metadata
type FunctionMeta struct {
	Description string                 `json:"description,omitempty"`
	Manifest    map[string]interface{} `json:"manifest,omitempty"`
}

// Memory represents a memory from the Open WebUI API
type Memory struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updated_at"`
	CreatedAt int64  `json:"created_at"`
}

// User represents a user from the Open WebUI API
type User struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Email           string                 `json:"email"`
	Username        string                 `json:"username,omitempty"`
	Role            string                 `json:"role"`
	ProfileImageURL string                 `json:"profile_image_url,omitempty"`
	Bio             string                 `json:"bio,omitempty"`
	Gender          string                 `json:"gender,omitempty"`
	DateOfBirth     string                 `json:"date_of_birth,omitempty"`
	Info            map[string]interface{} `json:"info,omitempty"`
	Settings        map[string]interface{} `json:"settings,omitempty"`
	APIKey          string                 `json:"api_key,omitempty"`
	OAuthSub        string                 `json:"oauth_sub,omitempty"`
	LastActiveAt    int64                  `json:"last_active_at,omitempty"`
	UpdatedAt       int64                  `json:"updated_at,omitempty"`
	CreatedAt       int64                  `json:"created_at,omitempty"`
}

// UserForm for creating/updating users
type UserForm struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	Role            string `json:"role"`
	ProfileImageURL string `json:"profile_image_url"`
}

// UserListResponse represents the paginated response from the users endpoint
type UserListResponse struct {
	Users []User `json:"users"`
	Total int    `json:"total"`
}

// Group represents a group from the Open WebUI API
type Group struct {
	ID          string                 `json:"id"`
	UserID      string                 `json:"user_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	UserIDs     []string               `json:"user_ids"`
	AdminIDs    []string               `json:"admin_ids,omitempty"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
}

// GroupForm for creating/updating groups
type GroupForm struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	UserIDs     []string `json:"user_ids,omitempty"`
}

// Feedback represents a feedback/evaluation from the Open WebUI API
type Feedback struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type,omitempty"`
	Data      map[string]interface{} `json:"data"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
	Snapshot  map[string]interface{} `json:"snapshot,omitempty"`
	CreatedAt int64                  `json:"created_at"`
	UpdatedAt int64                  `json:"updated_at"`
}

// FeedbackForm for creating/updating feedbacks
type FeedbackForm struct {
	Type     string                 `json:"type,omitempty"`
	Data     map[string]interface{} `json:"data"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
	Snapshot map[string]interface{} `json:"snapshot,omitempty"`
}

// BackupMetadata contains information about the backup
type BackupMetadata struct {
	OpenWebUIURL      string   `json:"open_webui_url"`
	OpenWebUIVersion  string   `json:"open_webui_version,omitempty"`
	BackupToolVersion string   `json:"backup_tool_version"`
	BackupTimestamp   string   `json:"backup_timestamp"`
	BackupType        string   `json:"backup_type"` // "knowledge", "model", "tool", "prompt", "file", "chat", "all"
	ItemCount         int      `json:"item_count"`
	UnifiedBackup     bool     `json:"unified_backup"`            // true for backup-all
	ContainedTypes    []string `json:"contained_types,omitempty"` // ["knowledge", "model", "tool", "prompt", "file", "chat", "user"]
}
