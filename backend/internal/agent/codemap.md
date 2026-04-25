# backend/internal/agent/

## Responsibility
Provides MCP (Model Context Protocol) handler functions for the CLI and agent interface. Exposes system operations (health checks, configuration, watchlists, libraries, jobs, stats) as callable functions for the MCP server.

## Design

### System Operations
- **ProbeSystem**: Health check for database, Slskd, Gonic connectivity
- **Bootstrap**: Environment validation, connectivity checks, schema migration

### Configuration
- **ReadConfig**: Returns static config + dynamic settings from DB
- **UpdateConfig**: Saves dynamic settings to DB
- **RegisterWebhook**: Sets notification callback URL

### Watchlists
- **ListWatchlists**: Returns all registered watchlists
- **AddWatchlist**: Creates new watchlist via WatchlistService
- **SyncWatchlist**: Triggers sync job for watchlist

### Libraries
- **ListLibraries**: Returns all libraries
- **AddLibrary**: Creates new library
- **DeleteLibrary**: Deletes library and associated tracks
- **ScanLibrary**: Triggers scan job for library

### Artists
- **ListMonitoredArtists**: Returns monitored artists with release counts

### Jobs
- **ListJobs**: Returns recent jobs (default limit: 50)
- **GetJobLogs**: Returns structured logs for a job
- **CancelJob**: Cancels queued/running job
- **RetryJob**: Retries failed job by resetting items to queued
- **EnqueueAcquisition**: Manually triggers acquisition job

### Search
- **SearchLibrary**: Searches local acquisitions + Gonic for tracks

### Statistics
- **GetJobStats**: Job counts by state (24h window)
- **GetLibraryStats**: Track counts, total size, format breakdown
- **GetStatsSummary**: Combined jobs + library + activity stats

### Quality Profiles
- **ListProfiles**: Returns all quality profiles
- **GetProfile**: Returns single profile by ID
- **CreateProfile**: Creates new profile (sets as default)
- **DeleteProfile**: Deletes profile (checks usage)
- **SetDefaultProfile**: Sets profile as default

### Types
- **SystemStatus**: Health status for database, Slskd, Gonic
- **JobStats**: Job counts and success rate
- **LibraryStats**: Track counts, size, format breakdown
- **ActivityStats**: Monitored artists, watchlists, libraries count
- **SummaryStats**: Combined stats

## Flow
1. **MCP Server** imports handlers and exposes functions via protocol
2. **CLI commands** call handlers directly for operations
3. **Handlers** interact with database via GORM and services
4. **Services** (WatchlistService, SlskdService, GonicClient) are injected

## Integration
- **Dependencies**: internal/database, internal/config, internal/services
- **Consumers**: cmd/agent/main.go (MCP server), CLI commands
