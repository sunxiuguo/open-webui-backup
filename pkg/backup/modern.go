package backup

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/vosiander/open-webui-backup/pkg/filearchive"
	"github.com/vosiander/open-webui-backup/pkg/knowledgearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"os"
	"path/filepath"
	"time"
)

func BackupModernKnowledge(client *openwebui.Client, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return err
	}
	name := time.Now().UTC().Format("20060102_150405") + "_knowledge_base_bundle.zip"
	return BackupModernKnowledgeFile(client, filepath.Join(outputDir, name), false)
}

func BackupModernKnowledgeFile(client *openwebui.Client, outputPath string, unified bool) error {
	bundle, err := knowledgearchive.Capture(client)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(outputPath)
		}
	}()
	w := zip.NewWriter(f)
	if err = knowledgearchive.Write(w, bundle); err != nil {
		w.Close()
		return err
	}
	meta := struct {
		*openwebui.BackupMetadata
		Format string `json:"format"`
	}{generateMetadata(client, "knowledge", len(bundle.Manifest.Knowledge), unified, []string{"knowledge"}), "modern-knowledge-v1"}
	entry, err := w.Create("owui.json")
	if err == nil {
		err = json.NewEncoder(entry).Encode(meta)
	}
	if err != nil {
		w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	complete = true
	logrus.Infof("Created owned-private root-only knowledge bundle: %s", outputPath)
	return nil
}

// backupSingleKnowledge backs up a single knowledge base to a ZIP file
func backupRepresentationFile(fileID string, client *openwebui.Client, outputDir string) error {
	b, err := filearchive.Capture(client, fileID)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s_file_%s_%s.zip", time.Now().UTC().Format("20060102_150405"), sanitizeFilename(b.Manifest.Filename), sanitizeFilename(fileID))
	path := filepath.Join(outputDir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(path)
		}
	}()
	w := zip.NewWriter(f)
	if err = filearchive.Write(w, "", b); err != nil {
		w.Close()
		return err
	}
	if err = writeMetadataToZip(w, generateMetadata(client, "file", 1, false, nil)); err != nil {
		w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	complete = true
	logrus.Infof("Created verified-representation file archive: %s", name)
	return nil
}

// BackupChats is the main entry point for backing up all chats
func backupRepresentationFileToZip(w *zip.Writer, fileID string, client *openwebui.Client) error {
	b, err := filearchive.Capture(client, fileID)
	if err != nil {
		return err
	}
	return filearchive.Write(w, "files/"+fileID+"/", b)
}

// BackupGroups is the main entry point for backing up all groups
func backupAllRepresentationFiles(zipWriter *zip.Writer, client *openwebui.Client) (int, error) {
	files, err := client.ListRepresentationFiles()
	if err != nil {
		return 0, fmt.Errorf("failed to list files: %w", err)
	}

	for i, fileMeta := range files {
		logrus.Infof("  Backing up file %d/%d: %s", i+1, len(files), fileMeta.Meta.Name)
		if err := backupRepresentationFileToZip(zipWriter, fileMeta.ID, client); err != nil {
			return 0, fmt.Errorf("file backup incomplete (%s): %w", fileMeta.ID, err)
		}
	}

	return len(files), nil
}

// backupFileToZip backs up a single file into an existing ZIP writer
// BackupRepresentationFiles is the explicit dual-representation file-only API.
func BackupRepresentationFiles(client *openwebui.Client, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return err
	}
	files, err := client.ListRepresentationFiles()
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := backupRepresentationFile(f.ID, client, outputDir); err != nil {
			return err
		}
	}
	return nil
}

// BackupRepresentationFilesUnified writes a file-only dual-representation ZIP.
func BackupRepresentationFilesUnified(client *openwebui.Client, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	w := zip.NewWriter(f)
	n, err := backupAllRepresentationFiles(w, client)
	if err != nil {
		w.Close()
		return err
	}
	if err = writeMetadataToZip(w, generateMetadata(client, "file", n, true, []string{"file"})); err != nil {
		w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
