package restore

import (
	"archive/zip"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
)

// isAuthError checks if an error is a 401 authentication error
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *openwebui.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 401
	}
	return false
}

// ProgressCallback is a function that receives progress updates during restore operations
type ProgressCallback func(percent int, message string)

// SelectiveRestoreOptions defines which data types to restore from a backup
type SelectiveRestoreOptions struct {
	Knowledge bool
	Models    bool
	Tools     bool
	Prompts   bool
	Files     bool
	Chats     bool
	Users     bool
	Groups    bool
	Feedbacks bool
}

// generateRandomPassword creates a cryptographically secure random password
func generateRandomPassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}
	// Use base64 encoding to ensure readable characters
	password := base64.URLEncoding.EncodeToString(bytes)
	// Trim to desired length
	if len(password) > length {
		password = password[:length]
	}
	return password, nil
}

// RestoreUser restores a user from a backup ZIP file
// Since passwords are not in the backup, a random password is generated
// If overwrite is true, existing user with same ID will be replaced
func RestoreUser(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting user restore from: %s", zipPath)

	// Extract user from ZIP
	user, err := extractUserFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract user from ZIP: %w", err)
	}

	logrus.Infof("Extracted user: %s (email: %s)", user.Name, user.Email)

	// Check if user already exists by email
	users, err := client.GetAllUsers()
	if err != nil {
		return fmt.Errorf("failed to check existing users: %w", err)
	}

	userExists := false
	for _, existingUser := range users {
		if existingUser.Email == user.Email {
			userExists = true
			break
		}
	}

	if userExists && !overwrite {
		return fmt.Errorf("user with email %s already exists (use --overwrite to replace)", user.Email)
	}

	// Generate random password (original password is not in backup)
	password, err := generateRandomPassword(16)
	if err != nil {
		return fmt.Errorf("failed to generate password: %w", err)
	}

	// Import the user
	logrus.Infof("Importing user: %s", user.Name)
	logrus.Warnf("Generated random password for user %s - user must reset password", user.Email)

	userForm := &openwebui.UserForm{
		Name:            user.Name,
		Email:           user.Email,
		Password:        password,
		Role:            user.Role,
		ProfileImageURL: user.ProfileImageURL,
	}

	if err := client.ImportUser(userForm); err != nil {
		return fmt.Errorf("failed to import user: %w", err)
	}

	logrus.Infof("Successfully restored user: %s (email: %s)", user.Name, user.Email)
	logrus.Infof("⚠️  User %s requires password reset", user.Email)
	return nil
}

// extractUserFromZip extracts the user.json from the ZIP file root
func extractUserFromZip(zipPath string) (*openwebui.User, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "user.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open user.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read user.json: %w", err)
			}

			var user openwebui.User
			if err := json.Unmarshal(data, &user); err != nil {
				return nil, fmt.Errorf("failed to parse user.json: %w", err)
			}

			return &user, nil
		}
	}

	return nil, fmt.Errorf("user.json not found in ZIP file")
}

// RestoreKnowledge restores a knowledge base from a backup ZIP file
// If overwrite is true, existing files will be replaced; otherwise they are skipped
func RestoreKnowledge(client *openwebui.Client, zipPath string, overwrite bool) error {
	if err := preflightKnowledgeArchive(client, zipPath); err != nil {
		return err
	}
	logrus.Infof("Starting restore from: %s", zipPath)

	// Extract knowledge base and files from ZIP
	kb, files, err := extractZipFile(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract ZIP file: %w", err)
	}

	logrus.Infof("Extracted knowledge base: %s", kb.Name)
	logrus.Infof("Found %d files to restore", len(files))

	// Check if knowledge base already exists
	existingKB, err := findKnowledgeByName(client, kb.Name)
	if err != nil {
		return fmt.Errorf("failed to check existing knowledge bases: %w", err)
	}

	var knowledgeID string
	if existingKB == nil {
		// Create new knowledge base
		logrus.Infof("Creating knowledge base: %s", kb.Name)
		form := &openwebui.KnowledgeForm{
			Name:          kb.Name,
			Description:   kb.Description,
			AccessControl: kb.AccessControl,
		}

		createResp, err := client.CreateKnowledge(form)
		if err != nil {
			return fmt.Errorf("failed to create knowledge base: %w", err)
		}

		knowledgeID = createResp.ID
		logrus.Infof("Knowledge base created with ID: %s", knowledgeID)

		// Upload and link all files
		fileIDMap, err := uploadFiles(client, files)
		if err != nil {
			return fmt.Errorf("failed to upload files: %w", err)
		}

		fileIDs := make([]string, 0, len(fileIDMap))
		for _, fileID := range fileIDMap {
			fileIDs = append(fileIDs, fileID)
		}

		if err := linkFilesToKnowledge(client, knowledgeID, fileIDs); err != nil {
			return fmt.Errorf("failed to link files to knowledge base: %w", err)
		}

		logrus.Infof("Successfully restored knowledge base: %s", kb.Name)
	} else {
		// Update existing knowledge base
		logrus.Infof("Knowledge base '%s' already exists (ID: %s)", kb.Name, existingKB.ID)
		knowledgeID = existingKB.ID

		// Update metadata if different
		if err := updateKnowledgeMetadata(client, existingKB, kb); err != nil {
			return fmt.Errorf("failed to update knowledge base metadata: %w", err)
		}

		// Get existing files
		existingFiles := getExistingFileMap(existingKB)
		logrus.Infof("Found %d existing files in knowledge base", len(existingFiles))

		// Sync files (upload new, overwrite if requested, skip otherwise)
		stats, err := syncFiles(client, knowledgeID, files, existingFiles, overwrite)
		if err != nil {
			return fmt.Errorf("failed to sync files: %w", err)
		}

		logrus.Infof("File sync completed: %d new, %d overwritten, %d skipped",
			stats.New, stats.Overwritten, stats.Skipped)
		logrus.Infof("Successfully restored knowledge base: %s", kb.Name)
	}

	return nil
}

// extractZipFile extracts knowledge base metadata and document files from ZIP
func extractZipFile(zipPath string) (*openwebui.KnowledgeBase, map[string][]byte, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	var kb *openwebui.KnowledgeBase
	files := make(map[string][]byte)

	for _, f := range r.File {
		// Read knowledge_base.json
		if f.Name == "knowledge_base.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to open knowledge_base.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read knowledge_base.json: %w", err)
			}

			kb = &openwebui.KnowledgeBase{}
			if err := json.Unmarshal(data, kb); err != nil {
				return nil, nil, fmt.Errorf("failed to parse knowledge_base.json: %w", err)
			}
			continue
		}

		// Read document files from documents/ folder
		if strings.HasPrefix(f.Name, "documents/") && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to open file %s: %w", f.Name, err)
			}

			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read file %s: %w", f.Name, err)
			}

			// Use only the filename, not the full path
			filename := filepath.Base(f.Name)
			files[filename] = content
		}
	}

	if kb == nil {
		return nil, nil, fmt.Errorf("knowledge_base.json not found in ZIP file")
	}

	return kb, files, nil
}

// checkKnowledgeExists checks if a knowledge base with the given name already exists
func checkKnowledgeExists(client *openwebui.Client, name string) (bool, error) {
	knowledgeBases, err := client.ListKnowledge()
	if err != nil {
		return false, err
	}

	for _, kb := range knowledgeBases {
		if strings.EqualFold(kb.Name, name) {
			return true, nil
		}
	}

	return false, nil
}

// uploadFiles uploads all document files and returns a map of filename to file ID
func uploadFiles(client *openwebui.Client, files map[string][]byte) (map[string]string, error) {
	fileIDMap := make(map[string]string)

	for filename, content := range files {
		logrus.Infof("Uploading file: %s (%d bytes)", filename, len(content))

		resp, err := client.UploadFile(filename, content)
		if err != nil {
			return nil, fmt.Errorf("failed to upload file %s: %w", filename, err)
		}

		fileIDMap[filename] = resp.ID
		logrus.Infof("File uploaded successfully: %s (ID: %s)", filename, resp.ID)
	}

	return fileIDMap, nil
}

// linkFilesToKnowledge associates all uploaded files with the knowledge base
func linkFilesToKnowledge(client *openwebui.Client, knowledgeID string, fileIDs []string) error {
	logrus.Infof("Linking %d files to knowledge base", len(fileIDs))

	for i, fileID := range fileIDs {
		logrus.Infof("Linking file %d/%d (ID: %s)", i+1, len(fileIDs), fileID)

		if err := client.AddFileToKnowledge(knowledgeID, fileID); err != nil {
			return fmt.Errorf("failed to link file %s: %w", fileID, err)
		}
	}

	logrus.Info("All files linked successfully")
	return nil
}

// findKnowledgeByName finds a knowledge base by name and returns it with full details
func findKnowledgeByName(client *openwebui.Client, name string) (*openwebui.KnowledgeBase, error) {
	knowledgeBases, err := client.ListKnowledge()
	if err != nil {
		return nil, err
	}

	for _, kb := range knowledgeBases {
		if strings.EqualFold(kb.Name, name) {
			// Get full details including files
			return client.GetKnowledgeByID(kb.ID)
		}
	}

	return nil, nil
}

// getExistingFileMap builds a map of filename to fileID from existing knowledge base
func getExistingFileMap(kb *openwebui.KnowledgeBase) map[string]string {
	fileMap := make(map[string]string)

	for _, file := range kb.Files {
		// Use the filename from meta.name
		if file.Meta.Name != "" {
			fileMap[file.Meta.Name] = file.ID
		}
	}

	return fileMap
}

// updateKnowledgeMetadata updates the knowledge base metadata if it differs from backup
func updateKnowledgeMetadata(client *openwebui.Client, existing, backup *openwebui.KnowledgeBase) error {
	needsUpdate := false

	if existing.Description != backup.Description {
		logrus.Infof("Description changed, updating...")
		needsUpdate = true
	}

	// Compare access control (simple comparison)
	if fmt.Sprintf("%v", existing.AccessControl) != fmt.Sprintf("%v", backup.AccessControl) {
		logrus.Infof("Access control changed, updating...")
		needsUpdate = true
	}

	if needsUpdate {
		form := &openwebui.KnowledgeForm{
			Name:          backup.Name,
			Description:   backup.Description,
			AccessControl: backup.AccessControl,
		}
		return client.UpdateKnowledge(existing.ID, form)
	}

	return nil
}

// SyncStats tracks file synchronization statistics
type SyncStats struct {
	New         int
	Overwritten int
	Skipped     int
}

// syncFiles synchronizes files between backup and existing knowledge base
func syncFiles(client *openwebui.Client, knowledgeID string, backupFiles map[string][]byte, existingFiles map[string]string, overwrite bool) (*SyncStats, error) {
	stats := &SyncStats{}

	for filename, content := range backupFiles {
		existingFileID, exists := existingFiles[filename]

		if !exists {
			// File doesn't exist, upload it
			logrus.Infof("Uploading new file: %s (%d bytes)", filename, len(content))

			resp, err := client.UploadFile(filename, content)
			if err != nil {
				return stats, fmt.Errorf("failed to upload file %s: %w", filename, err)
			}

			if err := client.AddFileToKnowledge(knowledgeID, resp.ID); err != nil {
				return stats, fmt.Errorf("failed to link file %s: %w", filename, err)
			}

			logrus.Infof("New file added: %s (ID: %s)", filename, resp.ID)
			stats.New++
		} else if overwrite {
			// File exists and overwrite is enabled
			logrus.Infof("Overwriting existing file: %s (%d bytes)", filename, len(content))

			// Remove old file from knowledge base
			if err := client.RemoveFileFromKnowledge(knowledgeID, existingFileID); err != nil {
				logrus.Warnf("Failed to remove old file %s: %v (continuing anyway)", filename, err)
			}

			// Upload new file
			resp, err := client.UploadFile(filename, content)
			if err != nil {
				return stats, fmt.Errorf("failed to upload file %s: %w", filename, err)
			}

			// Link new file
			if err := client.AddFileToKnowledge(knowledgeID, resp.ID); err != nil {
				return stats, fmt.Errorf("failed to link file %s: %w", filename, err)
			}

			logrus.Infof("File overwritten: %s (new ID: %s)", filename, resp.ID)
			stats.Overwritten++
		} else {
			// File exists but overwrite is disabled, skip it
			logrus.Infof("File already exists, skipping: %s", filename)
			stats.Skipped++
		}
	}

	return stats, nil
}

// RestoreModel restores a model from a backup ZIP file
// If overwrite is true, existing model with same ID will be replaced
func RestoreModel(client *openwebui.Client, zipPath string, overwrite bool) error {
	if err := preflightKnowledgeArchive(client, zipPath); err != nil {
		return err
	}
	logrus.Infof("Starting model restore from: %s", zipPath)

	// Extract model from ZIP
	model, err := extractModelFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract model from ZIP: %w", err)
	}

	logrus.Infof("Extracted model: %s (ID: %s)", model.Name, model.ID)

	// Check if model already exists
	existingModel, err := client.GetModelByID(model.ID)
	if err == nil && existingModel != nil && !overwrite {
		return fmt.Errorf("model with ID %s already exists (use --overwrite to replace)", model.ID)
	}

	// Restore knowledge bases and get ID mapping
	kbIDMap, err := restoreKnowledgeBasesFromZip(client, zipPath, overwrite)
	if err != nil {
		return fmt.Errorf("failed to restore knowledge bases: %w", err)
	}

	// Update model's knowledge base references with new IDs
	if len(kbIDMap) > 0 {
		updateModelKnowledgeReferences(model, kbIDMap)
	}

	// Import the model
	logrus.Infof("Importing model: %s", model.Name)
	models := []openwebui.Model{*model}
	if err := client.ImportModels(models); err != nil {
		return fmt.Errorf("failed to import model: %w", err)
	}

	logrus.Infof("Successfully restored model: %s", model.Name)
	return nil
}

// extractModelFromZip extracts the model.json from the ZIP file root
func extractModelFromZip(zipPath string) (*openwebui.Model, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "model.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open model.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read model.json: %w", err)
			}

			var model openwebui.Model
			if err := json.Unmarshal(data, &model); err != nil {
				return nil, fmt.Errorf("failed to parse model.json: %w", err)
			}

			return &model, nil
		}
	}

	return nil, fmt.Errorf("model.json not found in ZIP file")
}

// restoreKnowledgeBasesFromZip restores all knowledge items (files and collections) from ZIP
// Returns a map of old item ID to new item ID
func restoreKnowledgeBasesFromZip(client *openwebui.Client, zipPath string, overwrite bool) (map[string]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	idMap := make(map[string]string)

	// Restore file-type knowledge items
	fileIDMap, err := restoreFileKnowledgeItems(r, client)
	if err != nil {
		logrus.Warnf("Failed to restore file knowledge items: %v", err)
	}
	for oldID, newID := range fileIDMap {
		idMap[oldID] = newID
	}

	// Restore collection-type knowledge items (full KBs)
	collectionIDMap, err := restoreCollectionKnowledgeItems(r, client, overwrite)
	if err != nil {
		if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
			return nil, err
		}
		logrus.Warnf("Failed to restore collection knowledge items: %v", err)
	}
	for oldID, newID := range collectionIDMap {
		idMap[oldID] = newID
	}

	return idMap, nil
}

// restoreFileKnowledgeItems restores file-type knowledge items from model-files/ directory
func restoreFileKnowledgeItems(r *zip.ReadCloser, client *openwebui.Client) (map[string]string, error) {
	fileIDMap := make(map[string]string)

	// Map to track file items: fileID -> {metadata, content}
	fileData := make(map[string]struct {
		metadata map[string]interface{}
		content  []byte
		filename string
	})

	// Extract all file metadata and content
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "model-files/") {
			continue
		}

		parts := strings.Split(f.Name, "/")
		if len(parts) < 2 {
			continue
		}

		fileID := parts[1] // model-files/{file-id}/...

		// Initialize file entry if needed
		if _, exists := fileData[fileID]; !exists {
			fileData[fileID] = struct {
				metadata map[string]interface{}
				content  []byte
				filename string
			}{
				metadata: nil,
				content:  nil,
				filename: "",
			}
		}

		entry := fileData[fileID]

		// Read metadata.json
		if strings.HasSuffix(f.Name, "/metadata.json") {
			rc, err := f.Open()
			if err != nil {
				logrus.Warnf("Failed to open %s: %v", f.Name, err)
				continue
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				logrus.Warnf("Failed to read %s: %v", f.Name, err)
				continue
			}

			var metadata map[string]interface{}
			if err := json.Unmarshal(data, &metadata); err != nil {
				logrus.Warnf("Failed to parse %s: %v", f.Name, err)
				continue
			}

			entry.metadata = metadata
		}

		// Read file content
		if len(parts) >= 3 && parts[2] != "metadata.json" && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				logrus.Warnf("Failed to open file %s: %v", f.Name, err)
				continue
			}

			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				logrus.Warnf("Failed to read file %s: %v", f.Name, err)
				continue
			}

			entry.content = content
			entry.filename = filepath.Base(f.Name)
		}

		fileData[fileID] = entry
	}

	// Upload each file and build ID mapping
	for oldFileID, entry := range fileData {
		if entry.metadata == nil || entry.content == nil {
			logrus.Warnf("Incomplete file data for ID %s, skipping", oldFileID)
			continue
		}

		filename := entry.filename
		if filename == "" {
			filename = fmt.Sprintf("file_%s", oldFileID)
		}

		logrus.Infof("Restoring file: %s (original ID: %s)", filename, oldFileID)

		// Upload the file
		resp, err := client.UploadFile(filename, entry.content)
		if err != nil {
			logrus.Warnf("Failed to upload file %s: %v", filename, err)
			continue
		}

		fileIDMap[oldFileID] = resp.ID
		logrus.Infof("File uploaded with new ID: %s", resp.ID)
	}

	return fileIDMap, nil
}

// restoreCollectionKnowledgeItems restores collection-type knowledge items from knowledge-bases/ directory
func restoreCollectionKnowledgeItems(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) (map[string]string, error) {
	// Map to track knowledge bases found in ZIP: kbID -> {kb data, files}
	kbData := make(map[string]struct {
		kb    *openwebui.KnowledgeBase
		files map[string][]byte
	})

	// Extract all knowledge bases and their files
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "knowledge-bases/") {
			continue
		}

		parts := strings.Split(f.Name, "/")
		if len(parts) < 2 {
			continue
		}

		kbID := parts[1] // knowledge-bases/{kb-id}/...

		// Initialize KB entry if needed
		if _, exists := kbData[kbID]; !exists {
			kbData[kbID] = struct {
				kb    *openwebui.KnowledgeBase
				files map[string][]byte
			}{
				kb:    nil,
				files: make(map[string][]byte),
			}
		}

		entry := kbData[kbID]

		// Read knowledge_base.json
		if strings.HasSuffix(f.Name, "/knowledge_base.json") {
			rc, err := f.Open()
			if err != nil {
				logrus.Warnf("Failed to open %s: %v", f.Name, err)
				continue
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				logrus.Warnf("Failed to read %s: %v", f.Name, err)
				continue
			}

			var kb openwebui.KnowledgeBase
			if err := json.Unmarshal(data, &kb); err != nil {
				logrus.Warnf("Failed to parse %s: %v", f.Name, err)
				continue
			}

			entry.kb = &kb
		}

		// Read document files
		if strings.Contains(f.Name, "/documents/") && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				logrus.Warnf("Failed to open file %s: %v", f.Name, err)
				continue
			}

			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				logrus.Warnf("Failed to read file %s: %v", f.Name, err)
				continue
			}

			filename := filepath.Base(f.Name)
			entry.files[filename] = content
		}

		kbData[kbID] = entry
	}

	// Restore each knowledge base and build ID mapping
	kbIDMap := make(map[string]string)

	for oldKBID, entry := range kbData {
		if entry.kb == nil {
			logrus.Warnf("No knowledge_base.json found for KB ID %s, skipping", oldKBID)
			continue
		}

		logrus.Infof("Restoring knowledge base: %s (original ID: %s)", entry.kb.Name, oldKBID)

		// Check if knowledge base already exists by name
		existingKB, err := findKnowledgeByName(client, entry.kb.Name)
		if err != nil {
			logrus.Warnf("Failed to check for existing KB %s: %v", entry.kb.Name, err)
			continue
		}

		var newKBID string
		if existingKB == nil {
			// Create new knowledge base
			form := &openwebui.KnowledgeForm{
				Name:          entry.kb.Name,
				Description:   entry.kb.Description,
				AccessControl: entry.kb.AccessControl,
			}

			createResp, err := client.CreateKnowledge(form)
			if err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return nil, fmt.Errorf("failed to create KB %s: %w", entry.kb.Name, err)
				}
				logrus.Warnf("Failed to create KB %s: %v", entry.kb.Name, err)
				continue
			}

			newKBID = createResp.ID
			logrus.Infof("Created KB: %s (ID: %s)", entry.kb.Name, newKBID)

			// Upload files
			if len(entry.files) > 0 {
				fileIDMap, err := uploadFiles(client, entry.files)
				if err != nil {
					logrus.Warnf("Failed to upload files for KB %s: %v", entry.kb.Name, err)
				} else {
					fileIDs := make([]string, 0, len(fileIDMap))
					for _, fileID := range fileIDMap {
						fileIDs = append(fileIDs, fileID)
					}

					if err := linkFilesToKnowledge(client, newKBID, fileIDs); err != nil {
						logrus.Warnf("Failed to link files to KB %s: %v", entry.kb.Name, err)
					}
				}
			}
		} else {
			// Use existing knowledge base
			newKBID = existingKB.ID
			logrus.Infof("KB %s already exists (ID: %s)", entry.kb.Name, newKBID)

			// Update metadata if needed
			if err := updateKnowledgeMetadata(client, existingKB, entry.kb); err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return nil, fmt.Errorf("failed to update KB %s: %w", entry.kb.Name, err)
				}
				logrus.Warnf("Failed to update KB metadata for %s: %v", entry.kb.Name, err)
			}

			// Sync files if any
			if len(entry.files) > 0 {
				existingFiles := getExistingFileMap(existingKB)
				stats, err := syncFiles(client, newKBID, entry.files, existingFiles, overwrite)
				if err != nil {
					logrus.Warnf("Failed to sync files for KB %s: %v", entry.kb.Name, err)
				} else {
					logrus.Infof("File sync for KB %s: %d new, %d overwritten, %d skipped",
						entry.kb.Name, stats.New, stats.Overwritten, stats.Skipped)
				}
			}
		}

		// Map old ID to new ID
		kbIDMap[oldKBID] = newKBID
	}

	logrus.Infof("Restored %d knowledge bases", len(kbIDMap))
	return kbIDMap, nil
}

// updateModelKnowledgeReferences updates the model's knowledge base references with new IDs
func updateModelKnowledgeReferences(model *openwebui.Model, kbIDMap map[string]string) {
	if len(model.Meta.Knowledge) == 0 {
		return
	}

	logrus.Infof("Updating knowledge base references in model")

	// Update each knowledge base ID in the model
	for i := range model.Meta.Knowledge {
		item := model.Meta.Knowledge[i]
		oldID, ok := item["id"].(string)
		if !ok {
			logrus.Warnf("Knowledge item missing or invalid 'id' field, skipping")
			continue
		}

		if newID, found := kbIDMap[oldID]; found {
			logrus.Infof("Updating KB reference: %s -> %s", oldID, newID)
			model.Meta.Knowledge[i]["id"] = newID
		} else {
			logrus.Warnf("No mapping found for KB ID %s, keeping original", oldID)
		}
	}
}

// RestoreTool restores a tool from a backup ZIP file
// If overwrite is true, existing tool with same ID will be replaced
func RestoreTool(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting tool restore from: %s", zipPath)

	// Extract tool from ZIP
	tool, err := extractToolFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract tool from ZIP: %w", err)
	}

	logrus.Infof("Extracted tool: %s (ID: %s)", tool.Name, tool.ID)

	// Check if tool already exists by attempting to export and checking the list
	// (There's no direct "get tool by ID" endpoint, so we check the export list)
	tools, err := client.ExportTools()
	if err != nil {
		return fmt.Errorf("failed to check existing tools: %w", err)
	}

	toolExists := false
	for _, existingTool := range tools {
		if existingTool.ID == tool.ID {
			toolExists = true
			break
		}
	}

	if toolExists && !overwrite {
		return fmt.Errorf("tool with ID %s already exists (use --overwrite to replace)", tool.ID)
	}

	// Import the tool (API handles both create and update)
	logrus.Infof("Importing tool: %s", tool.Name)
	toolForm := &openwebui.ToolForm{
		ID:            tool.ID,
		Name:          tool.Name,
		Content:       tool.Content,
		Meta:          tool.Meta,
		AccessControl: tool.AccessControl,
	}

	if err := client.ImportTool(toolForm); err != nil {
		return fmt.Errorf("failed to import tool: %w", err)
	}

	logrus.Infof("Successfully restored tool: %s", tool.Name)
	return nil
}

// extractToolFromZip extracts the tool.json from the ZIP file root
func extractToolFromZip(zipPath string) (*openwebui.Tool, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "tool.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open tool.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read tool.json: %w", err)
			}

			var tool openwebui.Tool
			if err := json.Unmarshal(data, &tool); err != nil {
				return nil, fmt.Errorf("failed to parse tool.json: %w", err)
			}

			return &tool, nil
		}
	}

	return nil, fmt.Errorf("tool.json not found in ZIP file")
}

// RestorePrompt restores a prompt from a backup ZIP file
// If overwrite is true, existing prompt with same command will be replaced
func RestorePrompt(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting prompt restore from: %s", zipPath)

	// Extract prompt from ZIP
	prompt, err := extractPromptFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract prompt from ZIP: %w", err)
	}

	logrus.Infof("Extracted prompt: %s (command: %s)", prompt.Title, prompt.Command)

	// Check if prompt already exists by command
	prompts, err := client.ListPrompts()
	if err != nil {
		return fmt.Errorf("failed to check existing prompts: %w", err)
	}

	promptExists := false
	for _, existingPrompt := range prompts {
		if existingPrompt.Command == prompt.Command {
			promptExists = true
			break
		}
	}

	if promptExists && !overwrite {
		return fmt.Errorf("prompt with command '%s' already exists (use --overwrite to replace)", prompt.Command)
	}

	// Create/update the prompt (API handles both)
	logrus.Infof("Importing prompt: %s", prompt.Title)
	promptForm := &openwebui.PromptForm{
		Command:       prompt.Command,
		Title:         prompt.Title,
		Content:       prompt.Content,
		AccessControl: prompt.AccessControl,
	}

	if err := client.CreatePrompt(promptForm); err != nil {
		return fmt.Errorf("failed to import prompt: %w", err)
	}

	logrus.Infof("Successfully restored prompt: %s", prompt.Title)
	return nil
}

// extractPromptFromZip extracts the prompt.json from the ZIP file root
func extractPromptFromZip(zipPath string) (*openwebui.Prompt, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "prompt.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open prompt.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read prompt.json: %w", err)
			}

			var prompt openwebui.Prompt
			if err := json.Unmarshal(data, &prompt); err != nil {
				return nil, fmt.Errorf("failed to parse prompt.json: %w", err)
			}

			return &prompt, nil
		}
	}

	return nil, fmt.Errorf("prompt.json not found in ZIP file")
}

// RestoreChat restores a chat from a backup ZIP file
// If overwrite is true, existing chat with same ID will be replaced
func RestoreChat(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting chat restore from: %s", zipPath)

	// Extract chat from ZIP
	chat, err := extractChatFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract chat from ZIP: %w", err)
	}

	logrus.Infof("Extracted chat: %s (ID: %s)", chat.Title, chat.ID)

	// Check if chat already exists by ID
	existingChat, err := client.GetChatByID(chat.ID)
	if err == nil && existingChat != nil && !overwrite {
		return fmt.Errorf("chat with ID %s already exists (use --overwrite to replace)", chat.ID)
	}

	// Import the chat
	logrus.Infof("Importing chat: %s", chat.Title)
	if err := client.ImportChat(chat); err != nil {
		return fmt.Errorf("failed to import chat: %w", err)
	}

	logrus.Infof("Successfully restored chat: %s", chat.Title)
	return nil
}

// extractChatFromZip extracts the chat.json from the ZIP file root
func extractChatFromZip(zipPath string) (*openwebui.Chat, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "chat.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open chat.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read chat.json: %w", err)
			}

			var chat openwebui.Chat
			if err := json.Unmarshal(data, &chat); err != nil {
				return nil, fmt.Errorf("failed to parse chat.json: %w", err)
			}

			return &chat, nil
		}
	}

	return nil, fmt.Errorf("chat.json not found in ZIP file")
}

// RestoreFile restores a file from a backup ZIP file
// If overwrite is true, existing file with same ID will be replaced
func RestoreFile(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting file restore from: %s", zipPath)

	// Extract file from ZIP
	fileExport, err := extractFileFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract file from ZIP: %w", err)
	}

	logrus.Infof("Extracted file: %s (ID: %s)", fileExport.Meta.Name, fileExport.ID)

	// Check if file already exists by ID
	files, err := client.ListFiles()
	if err != nil {
		return fmt.Errorf("failed to check existing files: %w", err)
	}

	fileExists := false
	for _, existingFile := range files {
		if existingFile.ID == fileExport.ID {
			fileExists = true
			break
		}
	}

	if fileExists && !overwrite {
		return fmt.Errorf("file with ID %s already exists (use --overwrite to replace)", fileExport.ID)
	}

	// Import the file
	logrus.Infof("Importing file: %s (%d bytes)", fileExport.Meta.Name, len(fileExport.Data.Content))
	if err := client.CreateFileFromExport(fileExport); err != nil {
		return fmt.Errorf("failed to import file: %w", err)
	}

	logrus.Infof("Successfully restored file: %s", fileExport.Meta.Name)
	return nil
}

// extractFileFromZip extracts file.json and content from the ZIP file
func extractFileFromZip(zipPath string) (*openwebui.FileExport, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	var fileExport *openwebui.FileExport
	var fileContent []byte

	for _, f := range r.File {
		// Read file.json
		if f.Name == "file.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read file.json: %w", err)
			}

			fileExport = &openwebui.FileExport{}
			if err := json.Unmarshal(data, fileExport); err != nil {
				return nil, fmt.Errorf("failed to parse file.json: %w", err)
			}
			continue
		}

		// Read file content from content/ folder
		if strings.HasPrefix(f.Name, "content/") && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open content file %s: %w", f.Name, err)
			}

			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read content file %s: %w", f.Name, err)
			}

			fileContent = content
		}
	}

	if fileExport == nil {
		return nil, fmt.Errorf("file.json not found in ZIP file")
	}

	if fileContent == nil {
		return nil, fmt.Errorf("file content not found in ZIP file")
	}

	// Set the content in the export structure (convert []byte to string)
	fileExport.Data.Content = string(fileContent)

	return fileExport, nil
}

// RestoreSelective performs a selective restore from a unified backup file based on the provided options
// progressCallback is an optional callback function for progress updates (can be nil)
func RestoreSelective(client *openwebui.Client, inputFile string, options *SelectiveRestoreOptions, overwrite bool, progressCallback ProgressCallback) error {
	if options.Knowledge {
		if err := preflightKnowledgeArchive(client, inputFile); err != nil {
			return err
		}
	}
	logrus.Info("Starting selective restore...")

	if progressCallback != nil {
		progressCallback(0, "Starting selective restore...")
	}

	// Validate that at least one option is enabled
	if !options.Knowledge && !options.Models && !options.Tools && !options.Prompts && !options.Files && !options.Chats && !options.Users && !options.Groups && !options.Feedbacks {
		return fmt.Errorf("at least one data type must be selected for restore")
	}

	// Open the backup file
	r, err := zip.OpenReader(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer r.Close()

	// Read metadata
	var metadata *openwebui.BackupMetadata
	for _, f := range r.File {
		if f.Name == "owui.json" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to read metadata: %w", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("failed to read metadata: %w", err)
			}
			metadata = &openwebui.BackupMetadata{}
			if err := json.Unmarshal(data, metadata); err != nil {
				return fmt.Errorf("failed to parse metadata: %w", err)
			}
			break
		}
	}

	if metadata == nil {
		return fmt.Errorf("metadata not found in backup file (not a unified backup)")
	}

	if !metadata.UnifiedBackup {
		return fmt.Errorf("backup file is not a unified backup")
	}

	logrus.Infof("Restoring from unified backup with %d items", metadata.ItemCount)
	logrus.Infof("Available types: %v", metadata.ContainedTypes)

	// Restore selected types - USERS MUST BE RESTORED FIRST
	if options.Users {
		if progressCallback != nil {
			progressCallback(10, "Restoring users...")
		}
		if contains(metadata.ContainedTypes, "user") {
			logrus.Info("Restoring users...")
			if err := restoreUsersFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some users: %v", err)
			}
		} else {
			logrus.Info("Users not present in backup, skipping")
		}
	}

	if options.Knowledge {
		if progressCallback != nil {
			progressCallback(20, "Restoring knowledge bases...")
		}
		if contains(metadata.ContainedTypes, "knowledge") {
			logrus.Info("Restoring knowledge bases...")
			if err := restoreKnowledgeBasesFromUnified(r, client, overwrite); err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return err
				}
				logrus.Warnf("Failed to restore some knowledge bases: %v", err)
			}
		} else {
			logrus.Info("Knowledge bases not present in backup, skipping")
		}
	}

	if options.Models {
		if progressCallback != nil {
			progressCallback(35, "Restoring models...")
		}
		if contains(metadata.ContainedTypes, "model") {
			logrus.Info("Restoring models...")
			if err := restoreModelsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some models: %v", err)
			}
		} else {
			logrus.Info("Models not present in backup, skipping")
		}
	}

	if options.Tools {
		if progressCallback != nil {
			progressCallback(50, "Restoring tools...")
		}
		if contains(metadata.ContainedTypes, "tool") {
			logrus.Info("Restoring tools...")
			if err := restoreToolsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some tools: %v", err)
			}
		} else {
			logrus.Info("Tools not present in backup, skipping")
		}
	}

	if options.Prompts {
		if progressCallback != nil {
			progressCallback(60, "Restoring prompts...")
		}
		if contains(metadata.ContainedTypes, "prompt") {
			logrus.Info("Restoring prompts...")
			if err := restorePromptsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some prompts: %v", err)
			}
		} else {
			logrus.Info("Prompts not present in backup, skipping")
		}
	}

	if options.Files {
		if progressCallback != nil {
			progressCallback(70, "Restoring files...")
		}
		if contains(metadata.ContainedTypes, "file") {
			logrus.Info("Restoring files...")
			if err := restoreFilesFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some files: %v", err)
			}
		} else {
			logrus.Info("Files not present in backup, skipping")
		}
	}

	if options.Chats {
		if progressCallback != nil {
			progressCallback(78, "Restoring chats...")
		}
		if contains(metadata.ContainedTypes, "chat") {
			logrus.Info("Restoring chats...")
			if err := restoreChatsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some chats: %v", err)
			}
		} else {
			logrus.Info("Chats not present in backup, skipping")
		}
	}

	if options.Groups {
		if progressCallback != nil {
			progressCallback(86, "Restoring groups...")
		}
		if contains(metadata.ContainedTypes, "group") {
			logrus.Info("Restoring groups...")
			if err := restoreGroupsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some groups: %v", err)
			}
		} else {
			logrus.Info("Groups not present in backup, skipping")
		}
	}

	if options.Feedbacks {
		if progressCallback != nil {
			progressCallback(93, "Restoring feedbacks...")
		}
		if contains(metadata.ContainedTypes, "feedback") {
			logrus.Info("Restoring feedbacks...")
			if err := restoreFeedbacksFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some feedbacks: %v", err)
			}
		} else {
			logrus.Info("Feedbacks not present in backup, skipping")
		}
	}

	if progressCallback != nil {
		progressCallback(100, "Restore completed successfully")
	}
	logrus.Info("Selective restore completed successfully")
	return nil
}

// contains checks if a string slice contains a specific string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// RestoreAll restores data from either a unified backup ZIP or legacy separate ZIPs
func RestoreAll(client *openwebui.Client, inputDir string, overwrite bool) error {
	logrus.Info("Starting full restore...")

	// Get all ZIP files in directory
	files, err := filepath.Glob(filepath.Join(inputDir, "*.zip"))
	if err != nil {
		return fmt.Errorf("failed to list ZIP files: %w", err)
	}

	if len(files) == 0 {
		logrus.Info("No ZIP files found in directory")
		return nil
	}

	// Check for unified backup
	var unifiedBackup string
	for _, file := range files {
		if isUnifiedBackup(file) {
			unifiedBackup = file
			break
		}
	}

	if unifiedBackup != "" {
		// Restore from unified backup
		logrus.Infof("Detected unified backup: %s", filepath.Base(unifiedBackup))
		return restoreFromUnifiedBackup(client, unifiedBackup, overwrite)
	}

	// Legacy format: separate ZIP files
	logrus.Info("Using legacy restore mode (separate ZIP files)")
	return restoreFromLegacyBackups(client, files, overwrite)
}

// isUnifiedBackup checks if a ZIP file is a unified backup
func isUnifiedBackup(zipPath string) bool {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer r.Close()

	// Look for owui.json at root
	for _, f := range r.File {
		if f.Name == "owui.json" {
			rc, err := f.Open()
			if err != nil {
				return false
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return false
			}

			var metadata openwebui.BackupMetadata
			if err := json.Unmarshal(data, &metadata); err != nil {
				return false
			}

			return metadata.UnifiedBackup
		}
	}

	return false
}

// restoreFromUnifiedBackup restores all data from a unified backup ZIP
func restoreFromUnifiedBackup(client *openwebui.Client, zipPath string, overwrite bool) error {
	if err := preflightKnowledgeArchive(client, zipPath); err != nil {
		return err
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open unified backup: %w", err)
	}
	defer r.Close()

	// Read metadata
	var metadata *openwebui.BackupMetadata
	for _, f := range r.File {
		if f.Name == "owui.json" {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("failed to read metadata: %w", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("failed to read metadata: %w", err)
			}
			metadata = &openwebui.BackupMetadata{}
			if err := json.Unmarshal(data, metadata); err != nil {
				return fmt.Errorf("failed to parse metadata: %w", err)
			}
			break
		}
	}

	if metadata == nil {
		return fmt.Errorf("metadata not found in unified backup")
	}

	logrus.Infof("Restoring unified backup with %d items", metadata.ItemCount)
	logrus.Infof("Contained types: %v", metadata.ContainedTypes)

	// Restore each type present in the backup - USERS MUST BE RESTORED FIRST
	// First, restore users if present
	if contains(metadata.ContainedTypes, "user") {
		logrus.Info("Restoring users from unified backup...")
		if err := restoreUsersFromUnified(r, client, overwrite); err != nil {
			logrus.Warnf("Failed to restore some users: %v", err)
		}
	}

	// Then restore groups (after users, since groups reference users)
	if contains(metadata.ContainedTypes, "group") {
		logrus.Info("Restoring groups from unified backup...")
		if err := restoreGroupsFromUnified(r, client, overwrite); err != nil {
			logrus.Warnf("Failed to restore some groups: %v", err)
		}
	}

	// Then restore other types
	for _, dataType := range metadata.ContainedTypes {
		switch dataType {
		case "user", "group", "feedback":
			// Already restored or will be restored at the end
			continue
		case "knowledge":
			logrus.Info("Restoring knowledge bases from unified backup...")
			if err := restoreKnowledgeBasesFromUnified(r, client, overwrite); err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return err
				}
				logrus.Warnf("Failed to restore some knowledge bases: %v", err)
			}
		case "model":
			logrus.Info("Restoring models from unified backup...")
			if err := restoreModelsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some models: %v", err)
			}
		case "tool":
			logrus.Info("Restoring tools from unified backup...")
			if err := restoreToolsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some tools: %v", err)
			}
		case "prompt":
			logrus.Info("Restoring prompts from unified backup...")
			if err := restorePromptsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some prompts: %v", err)
			}
		case "file":
			logrus.Info("Restoring files from unified backup...")
			if err := restoreFilesFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some files: %v", err)
			}
		case "chat":
			logrus.Info("Restoring chats from unified backup...")
			if err := restoreChatsFromUnified(r, client, overwrite); err != nil {
				logrus.Warnf("Failed to restore some chats: %v", err)
			}
		}
	}

	// Finally, restore feedbacks at the end
	if contains(metadata.ContainedTypes, "feedback") {
		logrus.Info("Restoring feedbacks from unified backup...")
		if err := restoreFeedbacksFromUnified(r, client, overwrite); err != nil {
			logrus.Warnf("Failed to restore some feedbacks: %v", err)
		}
	}

	logrus.Info("Unified restore completed successfully")
	return nil
}

// restoreFromLegacyBackups restores from legacy separate ZIP files
func restoreFromLegacyBackups(client *openwebui.Client, files []string, overwrite bool) error {
	for _, file := range files {
		if err := preflightKnowledgeArchive(client, file); err != nil {
			return err
		}
	}
	var modelFiles, kbFiles, toolFiles, promptFiles, fileFiles, chatFiles []string

	for _, file := range files {
		basename := filepath.Base(file)
		if strings.Contains(basename, "_model_") {
			modelFiles = append(modelFiles, file)
		} else if strings.Contains(basename, "_knowledge_base_") {
			kbFiles = append(kbFiles, file)
		} else if strings.Contains(basename, "_tool_") {
			toolFiles = append(toolFiles, file)
		} else if strings.Contains(basename, "_prompt_") {
			promptFiles = append(promptFiles, file)
		} else if strings.Contains(basename, "_file_") {
			fileFiles = append(fileFiles, file)
		} else if strings.Contains(basename, "_chat_") {
			chatFiles = append(chatFiles, file)
		}
	}

	totalFiles := len(modelFiles) + len(kbFiles) + len(toolFiles) + len(promptFiles) + len(fileFiles) + len(chatFiles)
	logrus.Infof("Found %d backup file(s) to restore", totalFiles)

	// Restore in order: models, KBs, tools, prompts, files
	if len(modelFiles) > 0 {
		logrus.Infof("Restoring %d model(s)...", len(modelFiles))
		for i, file := range modelFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(modelFiles), filepath.Base(file))
			if err := RestoreModel(client, file, overwrite); err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return err
				}
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	if len(kbFiles) > 0 {
		logrus.Infof("Restoring %d knowledge base(s)...", len(kbFiles))
		for i, file := range kbFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(kbFiles), filepath.Base(file))
			if err := RestoreKnowledge(client, file, overwrite); err != nil {
				if errors.Is(err, openwebui.ErrKnowledgePermissionCompatibility) {
					return err
				}
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	if len(toolFiles) > 0 {
		logrus.Infof("Restoring %d tool(s)...", len(toolFiles))
		for i, file := range toolFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(toolFiles), filepath.Base(file))
			if err := RestoreTool(client, file, overwrite); err != nil {
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	if len(promptFiles) > 0 {
		logrus.Infof("Restoring %d prompt(s)...", len(promptFiles))
		for i, file := range promptFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(promptFiles), filepath.Base(file))
			if err := RestorePrompt(client, file, overwrite); err != nil {
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	if len(fileFiles) > 0 {
		logrus.Infof("Restoring %d file(s)...", len(fileFiles))
		for i, file := range fileFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(fileFiles), filepath.Base(file))
			if err := RestoreFile(client, file, overwrite); err != nil {
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	if len(chatFiles) > 0 {
		logrus.Infof("Restoring %d chat(s)...", len(chatFiles))
		for i, file := range chatFiles {
			logrus.Infof("  %d/%d: %s", i+1, len(chatFiles), filepath.Base(file))
			if err := RestoreChat(client, file, overwrite); err != nil {
				logrus.Warnf("  Failed: %v", err)
			}
		}
	}

	logrus.Info("Legacy restore completed successfully")
	return nil
}

// Helper functions for unified restore (these will extract from the already-open ZIP)
func restoreKnowledgeBasesFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	_, err := restoreCollectionKnowledgeItems(r, client, overwrite)
	return err
}

func restoreModelsFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	// Find all model directories
	modelDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "models/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				modelDirs[parts[1]] = true
			}
		}
	}

	for modelID := range modelDirs {
		if err := restoreSingleModelFromUnified(r, client, modelID, overwrite); err != nil {
			logrus.Warnf("  Failed to restore model %s: %v", modelID, err)
		}
	}
	return nil
}

func restoreSingleModelFromUnified(r *zip.ReadCloser, client *openwebui.Client, modelID string, overwrite bool) error {
	modelPath := fmt.Sprintf("models/%s/model.json", modelID)

	for _, f := range r.File {
		if f.Name == modelPath {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}

			var model openwebui.Model
			if err := json.Unmarshal(data, &model); err != nil {
				return err
			}

			// Check if exists
			existing, err := client.GetModelByID(model.ID)
			if err == nil && existing != nil && !overwrite {
				logrus.Infof("  Model %s already exists, skipping", model.Name)
				return nil
			}

			// Restore embedded KBs and files (they're in the same ZIP)
			// Import model
			logrus.Infof("  Restoring model: %s", model.Name)
			models := []openwebui.Model{model}
			if err := client.ImportModels(models); err != nil {
				if isAuthError(err) {
					return fmt.Errorf("authentication failed - please check your API key: %w", err)
				}
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("model.json not found for %s", modelID)
}

func restoreToolsFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	toolDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "tools/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				toolDirs[parts[1]] = true
			}
		}
	}

	for toolID := range toolDirs {
		toolPath := fmt.Sprintf("tools/%s/tool.json", toolID)
		for _, f := range r.File {
			if f.Name == toolPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var tool openwebui.Tool
				if err := json.Unmarshal(data, &tool); err != nil {
					continue
				}

				// Check if exists
				tools, _ := client.ExportTools()
				exists := false
				for _, t := range tools {
					if t.ID == tool.ID {
						exists = true
						break
					}
				}

				if exists && !overwrite {
					logrus.Infof("  Tool %s already exists, skipping", tool.Name)
					continue
				}

				logrus.Infof("  Restoring tool: %s", tool.Name)
				toolForm := &openwebui.ToolForm{
					ID:            tool.ID,
					Name:          tool.Name,
					Content:       tool.Content,
					Meta:          tool.Meta,
					AccessControl: tool.AccessControl,
				}
				client.ImportTool(toolForm)
			}
		}
	}
	return nil
}

func restorePromptsFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	promptDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "prompts/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				promptDirs[parts[1]] = true
			}
		}
	}

	for promptDir := range promptDirs {
		promptPath := fmt.Sprintf("prompts/%s/prompt.json", promptDir)
		for _, f := range r.File {
			if f.Name == promptPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var prompt openwebui.Prompt
				if err := json.Unmarshal(data, &prompt); err != nil {
					continue
				}

				// Check if exists
				prompts, _ := client.ListPrompts()
				exists := false
				for _, p := range prompts {
					if p.Command == prompt.Command {
						exists = true
						break
					}
				}

				if exists && !overwrite {
					logrus.Infof("  Prompt %s already exists, skipping", prompt.Title)
					continue
				}

				logrus.Infof("  Restoring prompt: %s", prompt.Title)
				promptForm := &openwebui.PromptForm{
					Command:       prompt.Command,
					Title:         prompt.Title,
					Content:       prompt.Content,
					AccessControl: prompt.AccessControl,
				}
				client.CreatePrompt(promptForm)
			}
		}
	}
	return nil
}

func restoreFilesFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	fileDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "files/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				fileDirs[parts[1]] = true
			}
		}
	}

	for fileID := range fileDirs {
		filePath := fmt.Sprintf("files/%s/file.json", fileID)
		var fileExport *openwebui.FileExport
		var fileContent []byte

		for _, f := range r.File {
			if f.Name == filePath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}
				fileExport = &openwebui.FileExport{}
				json.Unmarshal(data, fileExport)
			} else if strings.HasPrefix(f.Name, fmt.Sprintf("files/%s/content/", fileID)) {
				rc, _ := f.Open()
				fileContent, _ = io.ReadAll(rc)
				rc.Close()
			}
		}

		if fileExport != nil && fileContent != nil {
			// Check if exists
			files, _ := client.ListFiles()
			exists := false
			for _, f := range files {
				if f.ID == fileExport.ID {
					exists = true
					break
				}
			}

			if exists && !overwrite {
				logrus.Infof("  File %s already exists, skipping", fileExport.Meta.Name)
				continue
			}

			logrus.Infof("  Restoring file: %s", fileExport.Meta.Name)
			fileExport.Data.Content = string(fileContent)
			client.CreateFileFromExport(fileExport)
		}
	}
	return nil
}

func restoreChatsFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	chatDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "chats/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				chatDirs[parts[1]] = true
			}
		}
	}

	for chatID := range chatDirs {
		chatPath := fmt.Sprintf("chats/%s/chat.json", chatID)
		for _, f := range r.File {
			if f.Name == chatPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var chat openwebui.Chat
				if err := json.Unmarshal(data, &chat); err != nil {
					continue
				}

				// Check if exists
				existingChat, err := client.GetChatByID(chat.ID)
				if err == nil && existingChat != nil && !overwrite {
					logrus.Infof("  Chat %s already exists, skipping", chat.Title)
					continue
				}

				logrus.Infof("  Restoring chat: %s", chat.Title)
				if err := client.ImportChat(&chat); err != nil {
					if isAuthError(err) {
						return fmt.Errorf("authentication failed - please check your API key: %w", err)
					}
					logrus.Warnf("  Failed to restore chat %s: %v", chat.Title, err)
				}
			}
		}
	}
	return nil
}

func restoreUsersFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	userDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "users/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				userDirs[parts[1]] = true
			}
		}
	}

	for userID := range userDirs {
		userPath := fmt.Sprintf("users/%s/user.json", userID)
		for _, f := range r.File {
			if f.Name == userPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var user openwebui.User
				if err := json.Unmarshal(data, &user); err != nil {
					continue
				}

				// Check if user already exists by email
				users, err := client.GetAllUsers()
				if err != nil {
					logrus.Warnf("  Failed to check existing users: %v", err)
					continue
				}

				exists := false
				for _, existingUser := range users {
					if existingUser.Email == user.Email {
						exists = true
						break
					}
				}

				if exists && !overwrite {
					logrus.Infof("  User %s already exists, skipping", user.Email)
					continue
				}

				// Generate random password (original password is not in backup)
				password, err := generateRandomPassword(16)
				if err != nil {
					logrus.Warnf("  Failed to generate password for user %s: %v", user.Email, err)
					continue
				}

				logrus.Infof("  Restoring user: %s (email: %s)", user.Name, user.Email)
				logrus.Warnf("  Generated random password for user %s - user must reset password", user.Email)

				userForm := &openwebui.UserForm{
					Name:            user.Name,
					Email:           user.Email,
					Password:        password,
					Role:            user.Role,
					ProfileImageURL: user.ProfileImageURL,
				}

				if err := client.ImportUser(userForm); err != nil {
					if isAuthError(err) {
						return fmt.Errorf("authentication failed - please check your API key: %w", err)
					}
					logrus.Warnf("  Failed to restore user %s: %v", user.Email, err)
				} else {
					logrus.Infof("  ⚠️  User %s requires password reset", user.Email)
				}
			}
		}
	}
	return nil
}

// RestoreGroup restores a group from a backup ZIP file
// If overwrite is true, existing group with same ID will be replaced
func RestoreGroup(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting group restore from: %s", zipPath)

	// Extract group from ZIP
	group, err := extractGroupFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract group from ZIP: %w", err)
	}

	logrus.Infof("Extracted group: %s (ID: %s)", group.Name, group.ID)

	// Check if group already exists by ID
	existingGroup, err := client.GetGroupByID(group.ID)
	if err == nil && existingGroup != nil && !overwrite {
		return fmt.Errorf("group with ID %s already exists (use --overwrite to replace)", group.ID)
	}

	// Create the group
	logrus.Infof("Importing group: %s", group.Name)
	groupForm := &openwebui.GroupForm{
		Name:        group.Name,
		Description: group.Description,
		UserIDs:     group.UserIDs,
	}

	if _, err := client.CreateGroup(groupForm); err != nil {
		return fmt.Errorf("failed to import group: %w", err)
	}

	logrus.Infof("Successfully restored group: %s", group.Name)
	return nil
}

// extractGroupFromZip extracts the group.json from the ZIP file root
func extractGroupFromZip(zipPath string) (*openwebui.Group, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "group.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open group.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read group.json: %w", err)
			}

			var group openwebui.Group
			if err := json.Unmarshal(data, &group); err != nil {
				return nil, fmt.Errorf("failed to parse group.json: %w", err)
			}

			return &group, nil
		}
	}

	return nil, fmt.Errorf("group.json not found in ZIP file")
}

// restoreGroupsFromUnified restores groups from a unified backup
func restoreGroupsFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	groupDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "groups/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				groupDirs[parts[1]] = true
			}
		}
	}

	for groupID := range groupDirs {
		groupPath := fmt.Sprintf("groups/%s/group.json", groupID)
		for _, f := range r.File {
			if f.Name == groupPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var group openwebui.Group
				if err := json.Unmarshal(data, &group); err != nil {
					continue
				}

				// Check if group already exists by ID
				existingGroup, err := client.GetGroupByID(group.ID)
				if err == nil && existingGroup != nil && !overwrite {
					logrus.Infof("  Group %s already exists, skipping", group.Name)
					continue
				}

				logrus.Infof("  Restoring group: %s", group.Name)
				groupForm := &openwebui.GroupForm{
					Name:        group.Name,
					Description: group.Description,
					UserIDs:     group.UserIDs,
				}

				if _, err := client.CreateGroup(groupForm); err != nil {
					if isAuthError(err) {
						return fmt.Errorf("authentication failed - please check your API key: %w", err)
					}
					logrus.Warnf("  Failed to restore group %s: %v", group.Name, err)
				}
			}
		}
	}
	return nil
}

// RestoreFeedback restores a feedback from a backup ZIP file
// If overwrite is true, existing feedback with same ID will be replaced
func RestoreFeedback(client *openwebui.Client, zipPath string, overwrite bool) error {
	logrus.Infof("Starting feedback restore from: %s", zipPath)

	// Extract feedback from ZIP
	feedback, err := extractFeedbackFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract feedback from ZIP: %w", err)
	}

	logrus.Infof("Extracted feedback: ID %s", feedback.ID)

	// Check if feedback already exists by ID
	existingFeedback, err := client.GetFeedbackByID(feedback.ID)
	if err == nil && existingFeedback != nil && !overwrite {
		return fmt.Errorf("feedback with ID %s already exists (use --overwrite to replace)", feedback.ID)
	}

	// Create the feedback
	logrus.Infof("Importing feedback: %s", feedback.ID)
	feedbackForm := &openwebui.FeedbackForm{
		Type:     feedback.Type,
		Data:     feedback.Data,
		Meta:     feedback.Meta,
		Snapshot: feedback.Snapshot,
	}

	if _, err := client.CreateFeedback(feedbackForm); err != nil {
		return fmt.Errorf("failed to import feedback: %w", err)
	}

	logrus.Infof("Successfully restored feedback: %s", feedback.ID)
	return nil
}

// extractFeedbackFromZip extracts the feedback.json from the ZIP file root
func extractFeedbackFromZip(zipPath string) (*openwebui.Feedback, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "feedback.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open feedback.json: %w", err)
			}

			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read feedback.json: %w", err)
			}

			var feedback openwebui.Feedback
			if err := json.Unmarshal(data, &feedback); err != nil {
				return nil, fmt.Errorf("failed to parse feedback.json: %w", err)
			}

			return &feedback, nil
		}
	}

	return nil, fmt.Errorf("feedback.json not found in ZIP file")
}

// restoreFeedbacksFromUnified restores feedbacks from a unified backup
func restoreFeedbacksFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	feedbackDirs := make(map[string]bool)
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "feedbacks/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 2 {
				feedbackDirs[parts[1]] = true
			}
		}
	}

	for feedbackID := range feedbackDirs {
		feedbackPath := fmt.Sprintf("feedbacks/%s/feedback.json", feedbackID)
		for _, f := range r.File {
			if f.Name == feedbackPath {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					continue
				}

				var feedback openwebui.Feedback
				if err := json.Unmarshal(data, &feedback); err != nil {
					continue
				}

				// Check if feedback already exists by ID
				existingFeedback, err := client.GetFeedbackByID(feedback.ID)
				if err == nil && existingFeedback != nil && !overwrite {
					logrus.Infof("  Feedback %s already exists, skipping", feedback.ID)
					continue
				}

				logrus.Infof("  Restoring feedback: %s", feedback.ID)
				feedbackForm := &openwebui.FeedbackForm{
					Type:     feedback.Type,
					Data:     feedback.Data,
					Meta:     feedback.Meta,
					Snapshot: feedback.Snapshot,
				}

				if _, err := client.CreateFeedback(feedbackForm); err != nil {
					if isAuthError(err) {
						return fmt.Errorf("authentication failed - please check your API key: %w", err)
					}
					logrus.Warnf("  Failed to restore feedback %s: %v", feedback.ID, err)
				}
			}
		}
	}
	return nil
}
