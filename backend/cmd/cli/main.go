package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
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
	rootCmd.AddCommand(libraryCmd())
	rootCmd.AddCommand(profileCmd())
	rootCmd.AddCommand(statsCmd())

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
					fmt.Printf("- %s (%s): %s | ID: %s\n", l.Name, l.SourceType, l.SourceURI, l.ID)
				}
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add [name] [type] [uri]",
		Short: "Add a new watchlist",
		Args:  cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			spotifyAuth := api.NewSpotifyAuthHandler(db)
			service := services.NewWatchlistService(db, spotifyAuth, cfg)

			// Get default profile
			var profile database.QualityProfile
			if err := db.Where("is_default = ?", true).First(&profile).Error; err != nil {
				handleError(fmt.Errorf("no default quality profile found: %w", err))
				return
			}

			wl, err := agent.AddWatchlist(service, args[0], args[1], args[2], profile.ID, nil)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(wl)
			} else {
				fmt.Printf("Successfully added watchlist: %s (ID: %s)\n", wl.Name, wl.ID)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "sync [id]",
		Short: "Trigger synchronization for a watchlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			job, err := agent.SyncWatchlist(db, id, nil)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(job)
			} else {
				fmt.Printf("Synchronization job #%d enqueued.\n", job.ID)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "import",
		Short: "Import watchlists from JSON array via stdin",
		Long:  `Example: echo '[{"name": "My List", "source_type": "rss_feed", "source_uri": "...", "quality_profile_id": "..."}]' | netrunner-cli watchlist import`,
		Run: func(cmd *cobra.Command, args []string) {
			var inputs []struct {
				Name             string    `json:"name"`
				SourceType       string    `json:"source_type"`
				SourceURI        string    `json:"source_uri"`
				QualityProfileID uuid.UUID `json:"quality_profile_id"`
			}

			if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
				handleError(fmt.Errorf("failed to parse JSON from stdin: %w", err))
				return
			}

			spotifyAuth := api.NewSpotifyAuthHandler(db)
			service := services.NewWatchlistService(db, spotifyAuth, cfg)

			var created []database.Watchlist
			for _, input := range inputs {
				wl, err := agent.AddWatchlist(service, input.Name, input.SourceType, input.SourceURI, input.QualityProfileID, nil)
				if err != nil {
					handleError(fmt.Errorf("failed to import '%s': %w", input.Name, err))
					continue
				}
				created = append(created, *wl)
			}

			if jsonOutput {
				printJSON(created)
			} else {
				fmt.Printf("Successfully imported %d watchlists.\n", len(created))
			}
		},
	})

	return cmd
}

func libraryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage music libraries",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all libraries",
		Run: func(cmd *cobra.Command, args []string) {
			libraries, err := agent.ListLibraries(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(libraries)
			} else {
				if len(libraries) == 0 {
					fmt.Println("No libraries found.")
					return
				}
				for _, l := range libraries {
					fmt.Printf("- %s | Path: %s | ID: %s\n", l.Name, l.Path, l.ID)
				}
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add [name] [path]",
		Short: "Add a new library",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			library, err := agent.AddLibrary(db, args[0], args[1])
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(library)
			} else {
				fmt.Printf("Successfully added library: %s (ID: %s)\n", library.Name, library.ID)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "scan [id]",
		Short: "Trigger a scan for a library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			job, err := agent.ScanLibrary(db, id)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(job)
			} else {
				fmt.Printf("Successfully queued scan job: %d\n", job.ID)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			if err := agent.DeleteLibrary(db, id); err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(map[string]string{"status": "deleted"})
			} else {
				fmt.Println("Successfully deleted library.")
			}
		},
	})

	return cmd
}

func statsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show system statistics",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "summary",
		Short: "Show summary statistics",
		Run: func(cmd *cobra.Command, args []string) {
			stats, err := agent.GetStatsSummary(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(stats)
			} else {
				fmt.Printf("Jobs (24h):\n")
				fmt.Printf("  Total: %d | Queued: %d | Running: %d\n", stats.Jobs.Total, stats.Jobs.Queued, stats.Jobs.Running)
				fmt.Printf("  Succeeded: %d | Failed: %d | Success Rate: %.1f%%\n", stats.Jobs.Succeeded, stats.Jobs.Failed, stats.Jobs.SuccessRate)
				fmt.Printf("\nLibrary:\n")
				fmt.Printf("  Tracks: %d | Size: %.2f MB\n", stats.Library.TotalTracks, stats.Library.TotalSizeMB)
				fmt.Printf("\nActivity:\n")
				fmt.Printf("  Monitored Artists: %d\n", stats.Activity.MonitoredArtists)
				fmt.Printf("  Watchlists: %d\n", stats.Activity.Watchlists)
				fmt.Printf("  Libraries: %d\n", stats.Activity.Libraries)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "jobs",
		Short: "Show job statistics",
		Run: func(cmd *cobra.Command, args []string) {
			stats, err := agent.GetJobStats(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(stats)
			} else {
				fmt.Printf("Jobs (24h):\n")
				fmt.Printf("  Total: %d\n", stats.Total)
				fmt.Printf("  Queued: %d | Running: %d\n", stats.Queued, stats.Running)
				fmt.Printf("  Succeeded: %d | Failed: %d\n", stats.Succeeded, stats.Failed)
				fmt.Printf("  Success Rate: %.1f%%\n", stats.SuccessRate)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "library",
		Short: "Show library statistics",
		Run: func(cmd *cobra.Command, args []string) {
			stats, err := agent.GetLibraryStats(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(stats)
			} else {
				fmt.Printf("Library Statistics:\n")
				fmt.Printf("  Total Tracks: %d\n", stats.TotalTracks)
				fmt.Printf("  Total Size: %.2f MB\n", stats.TotalSizeMB)
				if len(stats.FormatBreakdown) > 0 {
					fmt.Printf("\n  Format Breakdown:\n")
					for _, f := range stats.FormatBreakdown {
						fmt.Printf("    %s: %d (%.2f MB)\n", f.Format, f.Count, float64(f.TotalSize)/(1024*1024))
					}
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

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage quality profiles",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all quality profiles",
		Run: func(cmd *cobra.Command, args []string) {
			profiles, err := agent.ListProfiles(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(profiles)
			} else {
				if len(profiles) == 0 {
					fmt.Println("No profiles found.")
					return
				}
				for _, p := range profiles {
					defaultMark := ""
					if p.IsDefault {
						defaultMark = " [DEFAULT]"
					}
					fmt.Printf("- %s%s\n", p.Name, defaultMark)
					fmt.Printf("  ID: %s\n", p.ID)
					if p.Description != "" {
						fmt.Printf("  %s\n", p.Description)
					}
					fmt.Printf("  Lossless: %v | Formats: %s | Min Bitrate: %d\n", p.PreferLossless, p.AllowedFormats, p.MinBitrate)
					fmt.Println()
				}
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add [name]",
		Short: "Add a new quality profile",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			description, _ := cmd.Flags().GetString("description")
			lossless, _ := cmd.Flags().GetBool("lossless")
			formats, _ := cmd.Flags().GetString("formats")
			minBitrate, _ := cmd.Flags().GetInt("min-bitrate")
			preferBitrate, _ := cmd.Flags().GetInt("prefer-bitrate")
			preferScene, _ := cmd.Flags().GetBool("scene")
			preferWeb, _ := cmd.Flags().GetBool("web")

			var pb *int
			if preferBitrate > 0 {
				pb = &preferBitrate
			}

			profile, err := agent.CreateProfile(db, args[0], description, lossless, formats, minBitrate, pb, preferScene, preferWeb)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(profile)
			} else {
				fmt.Printf("Successfully created profile: %s (ID: %s)\n", profile.Name, profile.ID)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a quality profile",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			if err := agent.DeleteProfile(db, id); err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(map[string]string{"status": "deleted"})
			} else {
				fmt.Println("Successfully deleted profile.")
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set-default [id]",
		Short: "Set a profile as the default",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			if err := agent.SetDefaultProfile(db, id); err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(map[string]string{"status": "updated"})
			} else {
				fmt.Println("Successfully set default profile.")
			}
		},
	})

	// Add flags to add command
	cmd.Commands()[1].Flags().String("description", "", "Profile description")
	cmd.Commands()[1].Flags().Bool("lossless", false, "Prefer lossless audio")
	cmd.Commands()[1].Flags().String("formats", "FLAC,ALAC,WAV", "Allowed formats (comma-separated)")
	cmd.Commands()[1].Flags().Int("min-bitrate", 0, "Minimum bitrate (kbps)")
	cmd.Commands()[1].Flags().Int("prefer-bitrate", 0, "Preferred bitrate (kbps)")
	cmd.Commands()[1].Flags().Bool("scene", false, "Prefer scene releases")
	cmd.Commands()[1].Flags().Bool("web", false, "Prefer web releases")

	return cmd
}

func handleError(err error) {
	if jsonOutput {
		printJSON(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
