package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

var dbPath string

func main() {
	rootCmd := &cobra.Command{
		Use:   "apikey",
		Short: "Manage Chinese Poetry API commercial keys",
	}

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "data/poetry.db", "Path to poetry SQLite database")
	rootCmd.AddCommand(createCmd(), listCmd(), updateCmd(), revokeCmd(), rebuildSearchCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func createCmd() *cobra.Command {
	var name string
	var tier string
	var dailyLimit int
	var notes string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			key, rawKey, err := repo.CreateAPIKey(database.CreateAPIKeyParams{
				Name:       name,
				Tier:       tier,
				DailyLimit: dailyLimit,
				Notes:      notes,
			})
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"id":          key.ID,
				"name":        key.Name,
				"tier":        key.Tier,
				"daily_limit": key.DailyLimit,
				"key_prefix":  key.KeyPrefix,
				"notes":       key.Notes,
				"api_key":     rawKey,
				"notice":      "store this api_key now; it will not be shown again",
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Customer or application name")
	cmd.Flags().StringVar(&tier, "tier", "developer", "Customer tier")
	cmd.Flags().IntVar(&dailyLimit, "daily-limit", 1000, "Daily quota; 0 means unlimited")
	cmd.Flags().StringVar(&notes, "notes", "", "Operator notes")

	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List API keys with today's usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			keys, err := repo.ListAPIKeysWithUsage()
			if err != nil {
				return err
			}

			return printJSON(keys)
		},
	}
}

func updateCmd() *cobra.Command {
	var name string
	var tier string
	var dailyLimit int
	var hasDailyLimit bool
	var enabled bool
	var hasEnabled bool
	var notes string
	var hasNotes bool

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an API key name, tier, daily limit, status, or notes",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			hasDailyLimit = cmd.Flags().Changed("daily-limit")
			hasEnabled = cmd.Flags().Changed("enabled")
			hasNotes = cmd.Flags().Changed("notes")
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var id int64
			if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id < 1 {
				return fmt.Errorf("invalid api key id: %s", args[0])
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			params := database.UpdateAPIKeyParams{}
			if name != "" {
				params.Name = &name
			}
			if tier != "" {
				params.Tier = &tier
			}
			if hasDailyLimit {
				params.DailyLimit = &dailyLimit
			}
			if hasEnabled {
				params.Enabled = &enabled
			}
			if hasNotes {
				params.Notes = &notes
			}

			key, err := repo.UpdateAPIKey(id, params)
			if err != nil {
				return err
			}

			return printJSON(key)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Customer or application name")
	cmd.Flags().StringVar(&tier, "tier", "", "Customer tier")
	cmd.Flags().IntVar(&dailyLimit, "daily-limit", 0, "Daily quota; 0 means unlimited")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable or disable the key")
	cmd.Flags().StringVar(&notes, "notes", "", "Operator notes")

	return cmd
}

func revokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id int64
			if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id < 1 {
				return fmt.Errorf("invalid api key id: %s", args[0])
			}

			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			if err := repo.RevokeAPIKey(id); err != nil {
				return err
			}

			return printJSON(map[string]any{"id": id, "revoked": true})
		},
	}
}

func rebuildSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild-search",
		Short: "Rebuild SQLite FTS5 search indexes for both languages",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, closeFn, err := openRepo()
			if err != nil {
				return err
			}
			defer closeFn()

			if err := repo.RebuildPoemFTSIndex(); err != nil {
				return err
			}
			if err := repo.WithLang(database.LangHant).RebuildPoemFTSIndex(); err != nil {
				return err
			}

			return printJSON(map[string]any{
				"rebuilt_languages": []string{string(database.LangHans), string(database.LangHant)},
			})
		},
	}
}

func openRepo() (*database.Repository, func(), error) {
	db, err := database.Open(dbPath, 1, 1)
	if err != nil {
		return nil, nil, err
	}

	if err := db.Migrate(); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return database.NewRepository(db), func() { _ = db.Close() }, nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
