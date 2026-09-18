package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

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
			slog.Error("Failed to load config", "error", err)
			os.Exit(1)
		}

		db, err = database.Connect(cfg)
		if err != nil {
			slog.Error("Failed to connect to database", "error", err)
			os.Exit(1)
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
				fmt.Printf("Database: %v\nLibrary: %v\nSlskd: %v\nMessage: %s\n",
					status.DatabaseConnected, status.LibraryConnected, status.SlskdConnected, status.Message)
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
		Use:   "prune [id]",
		Short: "Trigger a prune (remove missing files) for a library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid UUID: %w", err))
				return
			}

			job, err := agent.PruneLibrary(db, id)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(job)
			} else {
				fmt.Printf("Successfully queued prune job: %d\n", job.ID)
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

	cmd.AddCommand(&cobra.Command{
		Use:   "detect-fragments [libraryID]",
		Short: "Detect albums or artists split across legacy folders (credit and case variants)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var roots []database.Library
			if len(args) == 1 {
				id, err := uuid.Parse(args[0])
				if err != nil {
					handleError(fmt.Errorf("invalid library UUID: %w", err))
					return
				}
				var lib database.Library
				if err := db.First(&lib, "id = ?", id).Error; err != nil {
					handleError(fmt.Errorf("library not found: %w", err))
					return
				}
				roots = []database.Library{lib}
			} else {
				libs, err := agent.ListLibraries(db)
				if err != nil {
					handleError(err)
					return
				}
				roots = libs
			}

			// Each library's fragments are kept separate rather than merged into
			// one flat list: every suggested repair command takes a library id, so
			// aggregating across libraries left the operator with a literal
			// `<libraryID>` placeholder and no way to know which library to pass.
			type scannedLibrary struct {
				lib   database.Library
				frags *services.LibraryFragments
			}

			var scanned []scannedLibrary
			total := 0
			anyArtists, anyAlbums := false, false
			for _, lib := range roots {
				frags, err := services.DetectLibraryFragments(db, lib.Path)
				if err != nil {
					handleError(fmt.Errorf("library %s: %w", lib.Name, err))
					return
				}
				total += len(frags.Albums) + len(frags.Artists)
				anyArtists = anyArtists || len(frags.Artists) > 0
				anyAlbums = anyAlbums || len(frags.Albums) > 0
				scanned = append(scanned, scannedLibrary{lib: lib, frags: frags})
			}

			if jsonOutput {
				out := make([]libraryFragmentsJSON, 0, len(scanned))
				for _, s := range scanned {
					out = append(out, libraryFragmentsJSON{
						LibraryID:   s.lib.ID.String(),
						LibraryName: s.lib.Name,
						Albums:      s.frags.Albums,
						Artists:     s.frags.Artists,
					})
				}
				printJSON(out)
				return
			}

			if total == 0 {
				fmt.Println("No fragmented albums or artists found.")
				return
			}

			for _, s := range scanned {
				if len(s.frags.Albums) == 0 && len(s.frags.Artists) == 0 {
					continue
				}
				fmt.Printf("Library: %s (%s)\n\n", s.lib.Name, s.lib.ID)
				printLibraryFragments(s.lib.ID.String(), s.frags)
			}

			fmt.Println("Review the plan, then run the merge WITHOUT --apply first (dry run).")
			if anyArtists && anyAlbums {
				fmt.Println("Merge the artist splits first, then re-run detect-fragments before merging albums.")
			}
		},
	})

	mergeCmd := &cobra.Command{
		Use:   "merge-album <libraryID> <albumFolder> <canonicalArtistFolder>",
		Short: "Merge an album's per-credit artist folders into one (dry run unless --apply)",
		Args:  cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid library UUID: %w", err))
				return
			}
			var lib database.Library
			if err := db.First(&lib, "id = ?", id).Error; err != nil {
				handleError(fmt.Errorf("library not found: %w", err))
				return
			}

			apply, _ := cmd.Flags().GetBool("apply")
			report, err := services.MergeAlbumFolders(db, lib.Path, args[1], args[2], !apply)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(report)
			} else {
				printMergeReport(fmt.Sprintf("Album %q → %s", report.Album, report.CanonicalDir), report, apply)
			}
		},
	}
	mergeCmd.Flags().Bool("apply", false, "execute the merge (default is a dry run)")
	cmd.AddCommand(mergeCmd)

	mergeArtistCmd := &cobra.Command{
		Use:   "merge-artist <libraryID> <canonicalArtistFolder>",
		Short: "Merge an artist's case-variant folders into one (dry run unless --apply)",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid library UUID: %w", err))
				return
			}
			var lib database.Library
			if err := db.First(&lib, "id = ?", id).Error; err != nil {
				handleError(fmt.Errorf("library not found: %w", err))
				return
			}

			apply, _ := cmd.Flags().GetBool("apply")
			report, err := services.MergeArtistFolders(db, lib.Path, args[1], !apply)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(report)
			} else {
				printMergeReport(fmt.Sprintf("Artist %q → %s", args[1], report.CanonicalDir), report, apply)
			}
		},
	}
	mergeArtistCmd.Flags().Bool("apply", false, "execute the merge (default is a dry run)")
	cmd.AddCommand(mergeArtistCmd)

	repairTagsCmd := &cobra.Command{
		Use:   "repair-tags <libraryID>",
		Short: "Rewrite identity tags that disagree with the library's casing (dry run unless --apply)",
		Long: "Finds files whose album artist, album or track artist disagrees with the casing the\n" +
			"library already uses, and rewrites those tags. A client groups by tag, so a canonical\n" +
			"folder holding a peer-cased tag still lists one artist twice (DJI-494). Every file that\n" +
			"will be rewritten is copied into --backup-dir before the write; a backup inside the\n" +
			"library root is refused, because the media server would index it as a second copy.\n\n" +
			"For a whole-library operation, take the volume snapshot in\n" +
			"ops/docs/library-dedup-runbook.md first: the backup here covers only the rewritten files.",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id, err := uuid.Parse(args[0])
			if err != nil {
				handleError(fmt.Errorf("invalid library UUID: %w", err))
				return
			}
			var lib database.Library
			if err := db.First(&lib, "id = ?", id).Error; err != nil {
				handleError(fmt.Errorf("library not found: %w", err))
				return
			}

			apply, _ := cmd.Flags().GetBool("apply")
			backupDir, _ := cmd.Flags().GetString("backup-dir")

			ext := services.NewMetadataExtractor()
			report, err := services.PlanIdentityTagRepair(db, ext, lib.Path)
			if err != nil {
				handleError(err)
				return
			}

			if apply {
				if backupDir == "" {
					backupDir = services.DefaultTagBackupDir(lib.Path, time.Now())
				}
				if err := services.ApplyIdentityTagRepair(cmd.Context(), ext, lib.Path, backupDir, report); err != nil {
					handleError(err)
					return
				}
			}

			if jsonOutput {
				printJSON(report)
				return
			}
			printTagRepairReport(lib.Path, report, apply)

			// A partial run is not a success: a script must not read an
			// unrewritten file as a clean library.
			if len(report.Failures) > 0 {
				handleError(fmt.Errorf("%d file(s) could not be repaired", len(report.Failures)))
			}
		},
	}
	repairTagsCmd.Flags().Bool("apply", false, "execute the repair (default is a dry run)")
	repairTagsCmd.Flags().String("backup-dir", "", "directory to copy each rewritten file into (default: a timestamped sibling of the library root)")
	cmd.AddCommand(repairTagsCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "duplicates",
		Short: "List suspected duplicate recordings by MusicBrainz recording ID",
		Run: func(cmd *cobra.Command, args []string) {
			groups, err := agent.ListDuplicates(db)
			if err != nil {
				handleError(err)
				return
			}

			if jsonOutput {
				printJSON(groups)
			} else {
				if len(groups) == 0 {
					fmt.Println("No duplicate recordings found.")
					return
				}
				fmt.Printf("Found %d duplicate recording groups:\n\n", len(groups))
				for _, g := range groups {
					fmt.Printf("Recording ID: %s (%d copies)\n", g.MBRecordingID, len(g.Acquisitions))
					for _, a := range g.Acquisitions {
						fmt.Printf("  #%d  %s - %s  |  %s  |  %s  (score: %d%%)\n",
							a.ID, a.Artist, a.TrackTitle, a.FinalPath, formatFileSize(a.FileSize), a.AcoustIDScore)
					}
					fmt.Println()
				}
			}
		},
	})

	return cmd
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
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

// printMergeReport renders a merge report for both the album and the artist
// repair. Shared so one repair cannot drift from the other's output — the
// operator reads these to decide whether to pass --apply.
func printMergeReport(header string, report *services.MergeReport, apply bool) {
	mode := "DRY RUN (nothing changed; pass --apply to execute)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Printf("%s [%s]\n", header, mode)
	fmt.Printf("  moved: %d, duplicates removed: %d, dirs removed: %d, conflicts: %d, errors: %d\n\n",
		len(report.Moved), len(report.RemovedFiles), len(report.RemovedDirs), len(report.Conflicts), len(report.Errors))
	for _, m := range report.Moved {
		fmt.Printf("  MOVE  %s\n    -> %s\n", m.From, m.To)
	}
	for _, f := range report.RemovedFiles {
		fmt.Printf("  DEL   %s (identical copy exists at destination)\n", f)
	}
	for _, d := range report.RemovedDirs {
		fmt.Printf("  RMDIR %s\n", d)
	}
	for _, c := range report.Conflicts {
		fmt.Printf("  CONFLICT  %s\n", c)
	}
	for _, e := range report.Errors {
		fmt.Printf("  ERROR %s\n", e)
	}
	if apply && len(report.Errors) == 0 {
		fmt.Println("\nNext: trigger a media-server scan so the server re-indexes (ops/docs/library-dedup-runbook.md).")
	}
}

// printTagRepairReport renders the identity-tag repair plan, so the operator can
// judge the artist-count change before passing --apply.
func printTagRepairReport(libraryRoot string, report *services.TagRepairReport, apply bool) {
	mode := "DRY RUN (nothing changed; pass --apply to execute)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Printf("Identity tags for %s [%s]\n", libraryRoot, mode)
	fmt.Printf("  scanned: %d, unreadable: %d, files to rewrite: %d\n",
		report.Scanned, report.Unreadable, len(report.Fixes))
	fmt.Printf("  artists a client lists: %d -> %d\n", report.DistinctArtistsBefore, report.DistinctArtistsAfter)
	if len(report.ArtistsRemoved) > 0 {
		fmt.Printf("  no longer listed: %s\n", strings.Join(report.ArtistsRemoved, ", "))
	}
	fmt.Println()
	for _, f := range report.Fixes {
		fmt.Printf("  FIX   %s\n", f.File)
		if f.AlbumArtistFrom != "" {
			fmt.Printf("      album_artist %q -> %q\n", f.AlbumArtistFrom, f.AlbumArtistTo)
		}
		if f.AlbumFrom != "" {
			fmt.Printf("      album        %q -> %q\n", f.AlbumFrom, f.AlbumTo)
		}
		if f.TrackArtistFrom != "" {
			fmt.Printf("      artist       %q -> %q\n", f.TrackArtistFrom, f.TrackArtistTo)
		}
	}
	if apply {
		fmt.Printf("\n  backed up: %d -> %s, rewritten: %d\n", report.BackedUp, report.BackupDir, report.Rewritten)
	}
	for _, failure := range report.Failures {
		fmt.Printf("  ERROR %s\n", failure)
	}
	if len(report.Fixes) > 0 && (apply && len(report.Failures) == 0) {
		fmt.Println("\nNext: trigger a media-server scan so the server re-reads the tags (ops/docs/library-dedup-runbook.md).")
	}
	if !apply {
		fmt.Println("\nRe-run with --apply to rewrite these tags (files are copied to a backup first).")
	}
}

// libraryFragmentsJSON is the per-library shape printed by detect-fragments. It
// carries the library identity because every suggested merge command takes a
// library id — a flat aggregate of all libraries would not be actionable.
type libraryFragmentsJSON struct {
	LibraryID   string                      `json:"library_id"`
	LibraryName string                      `json:"library_name"`
	Albums      []services.FragmentedAlbum  `json:"albums"`
	Artists     []services.FragmentedArtist `json:"artists"`
}

// printLibraryFragments renders one library's fragments. libraryID is passed in
// rather than left as a placeholder so the merge commands it prints can be run
// verbatim. Artist splits come first because their repair is broader: merging
// the artist folder moves every album under it, so it can subsume album-level
// splits it does not name.
func printLibraryFragments(libraryID string, frags *services.LibraryFragments) {
	if len(frags.Artists) > 0 {
		fmt.Printf("Found %d artist(s) split across case-variant folders:\n\n", len(frags.Artists))
		for _, a := range frags.Artists {
			fmt.Printf("%s — %d track(s) across %d folder(s)\n", a.CanonicalFolder, a.TrackCount, len(a.Fragments))
			for _, f := range a.Fragments {
				marker := "    "
				if f.IsCanonical {
					marker = " ==>" // suggested merge target
				}
				fmt.Printf("%s %-45s (%d track(s))\n", marker, f.Folder, f.TrackCount)
			}
			fmt.Printf("    merge: netrunner-cli library merge-artist %s %q [--apply]\n\n",
				libraryID, a.CanonicalFolder)
		}
	}

	if len(frags.Albums) > 0 {
		fmt.Printf("Found %d fragmented album(s):\n\n", len(frags.Albums))
		for _, g := range frags.Albums {
			fmt.Printf("%s — %d track(s) across %d folder(s) [%s]\n",
				g.Album, g.TrackCount, len(g.Folders), g.Kind)
			for _, f := range g.Folders {
				marker := "    "
				if f.IsCanonical {
					marker = " ==>" // suggested merge target
				}
				// Both segments: a case-only split differs in the album
				// folder, so printing the artist alone is ambiguous.
				fmt.Printf("%s %s / %s (%d track(s))\n", marker, f.ArtistFolder, f.AlbumFolder, f.TrackCount)
			}
			fmt.Printf("    merge: netrunner-cli library merge-album %s %q %q [--apply]\n\n",
				libraryID, g.CanonicalAlbum, g.CanonicalFolder)
		}
	}
}

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage quality profiles",
	}

	// Define subcommands as variables for proper flag attachment
	listCmd := &cobra.Command{
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
	}

	addCmd := &cobra.Command{
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
	}

	rmCmd := &cobra.Command{
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
	}

	setDefaultCmd := &cobra.Command{
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
	}

	// Add commands to parent
	cmd.AddCommand(listCmd)
	cmd.AddCommand(addCmd)
	cmd.AddCommand(rmCmd)
	cmd.AddCommand(setDefaultCmd)

	// Add flags to add command
	addCmd.Flags().String("description", "", "Profile description")
	addCmd.Flags().Bool("lossless", false, "Prefer lossless audio")
	addCmd.Flags().String("formats", "FLAC,ALAC,WAV", "Allowed formats (comma-separated)")
	addCmd.Flags().Int("min-bitrate", 0, "Minimum bitrate (kbps)")
	addCmd.Flags().Int("prefer-bitrate", 0, "Preferred bitrate (kbps)")
	addCmd.Flags().Bool("scene", false, "Prefer scene releases")
	addCmd.Flags().Bool("web", false, "Prefer web releases")

	return cmd
}

var osExit = os.Exit // Make replaceable in tests

func handleError(err error) {
	if err != nil {
		if jsonOutput {
			printJSON(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		osExit(1)
	}
}
