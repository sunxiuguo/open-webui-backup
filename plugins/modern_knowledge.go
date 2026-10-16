package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/spf13/cobra"
	"github.com/vosiander/open-webui-backup/pkg/backup"
	"github.com/vosiander/open-webui-backup/pkg/config"
	"github.com/vosiander/open-webui-backup/pkg/encryption"
	"github.com/vosiander/open-webui-backup/pkg/openwebui"
	"github.com/vosiander/open-webui-backup/pkg/restore"
)

// NewModernKnowledgeCommand is deliberately separate from legacy/default/UI paths.
func NewModernKnowledgeCommand(cfg *config.Config) *cobra.Command {
	root := &cobra.Command{Use: "modern-knowledge", Short: "Opt-in experimental private root-only knowledge bundles (Open WebUI 0.12.2)", Long: "Experimental opt-in for Open WebUI 0.12.2 only. Owned-private, root-only knowledge graphs; no mixed data, overwrite, resume or automatic cleanup. Backup requires age encryption. Restore writes a private plaintext durable JSON report beside the encrypted input (or --report-dir), including partial IDs. Not real-backend accepted."}
	var output string
	var recipients []string
	b := &cobra.Command{Use: "backup", Short: "Capture and age-encrypt the complete supported knowledge graph", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if cfg.OpenWebUIAPIKey == "" {
			return fmt.Errorf("OPEN_WEBUI_API_KEY is required")
		}
		keys, err := encryption.GetEncryptRecipientsFromEnvOrFlag(recipients)
		if err != nil {
			return err
		}
		parsed := []age.Recipient{}
		for _, key := range keys {
			r, e := age.ParseX25519Recipient(key)
			if e != nil {
				return e
			}
			parsed = append(parsed, r)
		}
		if filepath.Ext(output) != ".age" {
			output += ".age"
		}
		// Private temporary directory holds plaintext; final output never overwrites.
		tmp, err := os.MkdirTemp(filepath.Dir(output), ".modern-kb-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		plain := filepath.Join(tmp, "bundle.zip")
		if err = backup.BackupModernKnowledgeFile(openwebui.NewClient(cfg.OpenWebUIURL, cfg.OpenWebUIAPIKey), plain, false); err != nil {
			return err
		}
		in, err := os.Open(plain)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		complete := false
		defer func() {
			out.Close()
			if !complete {
				os.Remove(output)
			}
		}()
		aw := armor.NewWriter(out)
		w, err := age.Encrypt(aw, parsed...)
		if err != nil {
			return err
		}
		if _, err = io.Copy(w, in); err != nil {
			return err
		}
		if err = w.Close(); err != nil {
			return err
		}
		if err = aw.Close(); err != nil {
			return err
		}
		if err = out.Sync(); err != nil {
			return err
		}
		if err = out.Close(); err != nil {
			return err
		}
		complete = true
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"status": "complete", "archive": output, "format": "modern-knowledge-v1+age"})
	}}
	b.Flags().StringVarP(&output, "out", "o", "", "Encrypted output path (.age appended); must not already exist")
	b.MarkFlagRequired("out")
	b.Flags().StringSliceVar(&recipients, "encrypt-recipient", nil, "Existing age recipients (or OWUI_ENCRYPTED_RECIPIENT)")
	var input, reportDir string
	var identities []string
	r := &cobra.Command{Use: "restore", Short: "Restore into new objects and persist a verified or partial result", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if cfg.OpenWebUIAPIKey == "" {
			return fmt.Errorf("OPEN_WEBUI_API_KEY is required")
		}
		paths, err := encryption.GetDecryptIdentityFilesFromEnvOrFlag(identities)
		if err != nil {
			return err
		}
		contents := []string{}
		for _, path := range paths {
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			contents = append(contents, string(data))
		}
		tmp, err := os.MkdirTemp("", "modern-kb-restore-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		plain := filepath.Join(tmp, "bundle.zip")
		if err = encryption.DecryptFile(input, plain, &encryption.DecryptOptions{Identities: contents}); err != nil {
			return err
		}
		if reportDir == "" {
			reportDir = filepath.Dir(input)
		}
		path, result, restoreErr := restore.RestoreModernKnowledgeToReportDir(openwebui.NewClient(cfg.OpenWebUIURL, cfg.OpenWebUIAPIKey), plain, reportDir)
		status := result.Status
		if status == "" {
			status = "rejected"
		}
		payload := map[string]interface{}{"status": status, "report_path": path, "result": result}
		if restoreErr != nil {
			payload["error"] = restoreErr.Error()
		}
		if err = json.NewEncoder(cmd.OutOrStdout()).Encode(payload); err != nil {
			return err
		}
		return restoreErr
	}}
	r.Flags().StringVarP(&input, "file", "f", "", "Age-encrypted modern knowledge archive")
	r.MarkFlagRequired("file")
	r.Flags().StringSliceVar(&identities, "decrypt-identity", nil, "Existing age identity files (or OWUI_DECRYPT_IDENTITY)")
	r.Flags().StringVar(&reportDir, "report-dir", "", "Existing persistent directory for private plaintext JSON reports; defaults beside input")
	root.AddCommand(b, r)
	return root
}
