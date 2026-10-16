package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vosiander/open-webui-backup/pkg/config"
	"github.com/vosiander/open-webui-backup/pkg/plugin"
	"github.com/vosiander/open-webui-backup/plugins"
)

func main() {
	// Configure logrus
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	if err := run(); err != nil {
		logrus.WithError(err).Fatal("Application error")
	}
}

func run() error {
	// Load configuration from environment variables
	cfg := config.Load()

	// Create plugin registry
	registry := plugin.NewRegistry()

	// Register unified backup and restore plugins
	registry.Register(plugins.NewBackupPlugin())
	registry.Register(plugins.NewRestorePlugin())
	registry.Register(plugins.NewPurgePlugin())

	// Register age encryption and backup management plugins
	registry.Register(plugins.NewNewIdentityPlugin())
	registry.Register(plugins.NewVerifyPlugin())
	registry.Register(plugins.NewFullBackupPlugin())
	registry.Register(plugins.NewDecryptPlugin())
	registry.Register(plugins.NewStatisticsPlugin())
	registry.Register(plugins.NewChatsPlugin())

	// Register database backup plugins
	registry.Register(plugins.NewBackupDatabasePlugin())
	registry.Register(plugins.NewPurgeDatabasePlugin())
	registry.Register(plugins.NewRestoreDatabasePlugin())

	// Create root command
	rootCmd := &cobra.Command{
		Use:   "owuicli",
		Short: "Open WebUI Backup CLI Tool",
		Long:  "Command-line tool to backup and restore various important information from an Open WebUI application",
	}

	// Register all plugin commands
	for _, p := range registry.GetPlugins() {
		cmd := plugin.CreateCommand(p, cfg)
		rootCmd.AddCommand(cmd)
	}

	rootCmd.AddCommand(plugins.NewModernKnowledgeCommand(cfg))

	// Execute root command
	return rootCmd.Execute()
}
