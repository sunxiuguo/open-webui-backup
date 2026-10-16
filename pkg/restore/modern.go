package restore

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/vosiander/open-webui-backup/pkg/filearchive"
	"github.com/vosiander/open-webui-backup/pkg/knowledgearchive"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"path/filepath"
	"strings"
)

func RestoreModernKnowledge(client *openwebui.Client, zipPath string, overwrite bool) error {
	reportPath, _, err := RestoreModernKnowledgeWithReport(client, zipPath, overwrite)
	if err != nil {
		return err
	}
	logrus.Infof("Knowledge bundle verified; restore report: %s", reportPath)
	return nil
}

func RestoreModernKnowledgeWithReport(client *openwebui.Client, zipPath string, overwrite bool) (string, knowledgearchive.Report, error) {
	if overwrite {
		return "", knowledgearchive.Report{}, fmt.Errorf("modern knowledge restore never overwrites existing resources")
	}
	return RestoreModernKnowledgeToReportDir(client, zipPath, filepath.Dir(zipPath))
}

// RestoreModernKnowledgeToReportDir keeps reports outside any ephemeral decrypted input.
func RestoreModernKnowledgeToReportDir(client *openwebui.Client, zipPath, reportDir string) (string, knowledgearchive.Report, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", knowledgearchive.Report{}, err
	}
	defer r.Close()
	b, err := knowledgearchive.Read(&r.Reader)
	if err != nil {
		return "", knowledgearchive.Report{}, err
	}
	return knowledgearchive.Restore(client, b, reportDir)
}

// extractZipFile extracts knowledge base metadata and document files from ZIP
func RestoreFileWithResult(client *openwebui.Client, zipPath string, overwrite bool) (filearchive.RestoreResult, error) {
	if err := RejectModernKnowledgeArchive(zipPath); err != nil {
		return filearchive.RestoreResult{}, err
	}
	if overwrite {
		return filearchive.RestoreResult{}, fmt.Errorf("dual-representation restore does not support overwrite")
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return filearchive.RestoreResult{}, err
	}
	defer r.Close()
	b, err := filearchive.Read(&r.Reader, "")
	if err != nil {
		return filearchive.RestoreResult{}, err
	}
	return filearchive.Restore(client, b)
}

// extractFileFromZip extracts file.json and content from the ZIP file
func restoreRepresentationFilesFromUnified(r *zip.ReadCloser, client *openwebui.Client, overwrite bool) error {
	if overwrite {
		return fmt.Errorf("dual-representation restore does not support overwrite")
	}
	dirs := map[string]bool{}
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "files/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 3 {
				dirs["files/"+parts[1]+"/"] = true
			}
		}
	}
	if len(dirs) == 0 {
		return fmt.Errorf("no file representations in archive")
	}
	// Validate every archive member before making the first remote write.
	bundles := []*filearchive.Bundle{}
	for dir := range dirs {
		b, err := filearchive.Read(&r.Reader, dir)
		if err != nil {
			return err
		}
		if dir != "files/"+b.Manifest.SourceID+"/" {
			return fmt.Errorf("file directory and source ID mismatch")
		}
		if b.Indexed == "" {
			return fmt.Errorf("empty indexed text is unsupported by the pinned processing API")
		}
		if b.Manifest.IndexedState != "present" {
			return fmt.Errorf("unsupported indexed state %s", b.Manifest.IndexedState)
		}
		bundles = append(bundles, b)
	}
	for _, b := range bundles {
		result, err := filearchive.Restore(client, b)
		if err != nil {
			return err
		}
		logrus.Infof("Verified file representations: %s -> %s (metadata retained in archive; associations not restored)", result.SourceID, result.NewID)
	}
	return nil
}

func RejectModernKnowledgeArchive(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := filearchive.ValidateArchivePaths(&r.Reader); err != nil {
		return err
	}
	for _, f := range r.File {
		if f.Name == "knowledge-manifest.json" {
			return fmt.Errorf("modern knowledge bundle requires explicit modern-knowledge restore")
		}

		if f.Name == "owui.json" {
			rc, e := f.Open()
			if e != nil {
				return e
			}
			var meta struct {
				Format string `json:"format"`
			}
			e = json.NewDecoder(rc).Decode(&meta)
			rc.Close()
			if e != nil {
				return e
			}
			if strings.HasPrefix(meta.Format, "modern-knowledge") {
				return fmt.Errorf("modern knowledge bundle requires explicit modern-knowledge restore")
			}
		}
	}
	return nil
}

func RestoreRepresentationFile(client *openwebui.Client, path string, overwrite bool) error {
	_, err := RestoreFileWithResult(client, path, overwrite)
	return err
}
func RestoreRepresentationFilesUnified(client *openwebui.Client, path string, overwrite bool) error {
	if err := RejectModernKnowledgeArchive(path); err != nil {
		return err
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if err = filearchive.ValidateArchivePaths(&r.Reader); err != nil {
		return err
	}
	var meta openwebui.BackupMetadata
	found := false
	for _, f := range r.File {
		if f.Name == "owui.json" {
			rc, e := f.Open()
			if e != nil {
				return e
			}
			e = json.NewDecoder(rc).Decode(&meta)
			rc.Close()
			if e != nil {
				return e
			}
			found = true
		}
	}
	if !found || !meta.UnifiedBackup || !contains(meta.ContainedTypes, "file") {
		return fmt.Errorf("file-only unified metadata required")
	}
	for _, typ := range meta.ContainedTypes {
		if typ != "file" {
			return fmt.Errorf("mixed representation restore unsupported")
		}
	}
	return restoreRepresentationFilesFromUnified(r, client, overwrite)
}

// rejectOptInArchive prevents the legacy importer from flattening either new format.
func rejectOptInArchive(path string) error {
	if err := RejectModernKnowledgeArchive(path); err != nil {
		return err
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "representations.json" || strings.HasSuffix(f.Name, "/representations.json") {
			return fmt.Errorf("dual-representation archive requires explicit representation restore API")
		}
	}
	return nil
}
