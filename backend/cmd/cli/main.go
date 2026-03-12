package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/pvnkmnk/netrunner/backend/internal/agent"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	jsonOutput bool
	db         *gorm.DB
	cfg        *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "netrunner-cli",
	Short: "NetRunner Agent-Native CLI",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var err error
		cfg, err = config.Load()
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}

		db, err = database.Connect(cfg)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
	},
}

func main() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Subcommands
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(watchlistCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check system status",
		Run: func(cmd *cobra.Command, args []string) {
			status, err := agent.ProbeSystem(db, cfg)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(status)
			} else {
				fmt.Printf("Database: %v\nGonic: %v\nSlskd: %v\nMessage: %s\n",
					status.DatabaseConnected, status.GonicConnected, status.SlskdConnected, status.Message)
			}
		},
	}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage system configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all configuration settings",
		Run: func(cmd *cobra.Command, args []string) {
			settings, err := agent.ReadConfig(db, cfg)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(settings)
			} else {
				for k, v := range settings {
					fmt.Printf("%s: %s\n", k, v)
				}
			}
		},
	})

	return cmd
}

func watchlistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchlist",
		Short: "Manage music discovery watchlists",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all watchlists",
		Run: func(cmd *cobra.Command, args []string) {
			spotifyAuth := api.NewSpotifyAuthHandler(db)
			service := services.NewWatchlistService(db, spotifyAuth, cfg)
			lists, err := agent.ListWatchlists(service)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(lists)
			} else {
				for _, l := range lists {
					fmt.Printf("- %s (%s): %s\n", l.Name, l.SourceType, l.SourceURI)
				}
			}
		},
	})

	return cmd
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func handleError(err error) {
	if jsonOutput {
		printJSON(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
