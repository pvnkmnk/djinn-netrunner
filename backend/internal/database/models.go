package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JSONStringArray is a custom type for storing string arrays as JSON in SQLite.
type JSONStringArray []string

// Value implements the driver.Valuer interface.
func (a JSONStringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

// Scan implements the sql.Scanner interface.
func (a *JSONStringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("cannot scan type into JSONStringArray")
	}

	return json.Unmarshal(bytes, a)
}

// FilterModeType represents the filter mode for quality profiles.
type FilterModeType string

const (
	FilterModePreferred FilterModeType = "preferred"
	FilterModeRequired  FilterModeType = "required"
)

// User represents a user in the system
type User struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	Role         string `gorm:"default:'user'"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time

	Sessions []Session `gorm:"foreignKey:UserID"`
}

// Session represents a user session
type Session struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	SessionID string `gorm:"uniqueIndex;not null"`
	UserID    uint64 `gorm:"not null;index"`
	CreatedAt time.Time
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

// QualityProfile defines download preferences
type QualityProfile struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name                string    `gorm:"uniqueIndex;not null"`
	Description         string
	PreferLossless      bool   `gorm:"default:true"`
	AllowedFormats      string `gorm:"type:text"` // Store as comma-separated or JSON for portability
	MinBitrate          int    `gorm:"default:320"`
	PreferBitrate       *int
	PreferSceneReleases bool   `gorm:"default:false"`
	PreferWebReleases   bool   `gorm:"default:true"`
	CoverArtSources     string `gorm:"default:'source,musicbrainz,discogs'"` // comma-separated priority list
	IsDefault           bool   `gorm:"default:false"`
	OwnerUserID         *uint64 `gorm:"index"`

	// Advanced filtering (Phase 2)
	MinSampleRate         int             `gorm:"default:0"`           // e.g. 44100, 48000
	MinBitDepth           int             `gorm:"default:0"`           // e.g. 16, 24
	FormatPreferenceOrder JSONStringArray `gorm:"type:text"`           // JSON array: ["flac","wav","alac","mp3"]
	FilterMode            FilterModeType  `gorm:"default:'preferred'"` // "preferred" or "required"
	MaxPeerQueueDepth     int             `gorm:"default:0"`           // 0 = no limit
	TranscodeTarget       string          `gorm:"default:''"`          // Target format for transcoding (e.g. "opus", "mp3", "aac")

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (m *QualityProfile) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// MonitoredArtist represents an artist being tracked
type MonitoredArtist struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	MusicBrainzID  string    `gorm:"uniqueIndex;not null"`
	Name           string    `gorm:"not null"`
	SortName       string
	Disambiguation string

	QualityProfileID uuid.UUID `gorm:"type:uuid;not null;index"`
	Monitored        bool      `gorm:"default:true"`
	MonitorNew       bool      `gorm:"column:monitor_new_releases;default:true"`

	MonitorAlbums       bool `gorm:"default:true"`
	MonitorEPs          bool `gorm:"default:true"`
	MonitorSingles      bool `gorm:"default:false"`
	MonitorCompilations bool `gorm:"default:false"`
	MonitorLive         bool `gorm:"default:false"`

	LastScanDate     *time.Time
	LastReleaseCheck *time.Time
	TotalReleases    int `gorm:"default:0"`
	AcquiredReleases int `gorm:"default:0"`

	CreatedAt   time.Time
	UpdatedAt   time.Time
	OwnerUserID *uint64 `gorm:"index"`

	QualityProfile QualityProfile   `gorm:"foreignKey:QualityProfileID"`
	Releases       []TrackedRelease `gorm:"foreignKey:ArtistID"`
}

func (m *MonitoredArtist) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// TrackedRelease represents a release we are monitoring
type TrackedRelease struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey"`
	ArtistID uuid.UUID `gorm:"type:uuid;not null;index"`

	ReleaseGroupID string `gorm:"column:musicbrainz_release_group_id;not null"`
	ReleaseID      string `gorm:"column:musicbrainz_release_id"`

	Title         string `gorm:"not null"`
	ReleaseType   string `gorm:"not null"`
	ReleaseDate   *time.Time
	ReleaseStatus string `gorm:"default:'official'"`

	Status    string `gorm:"default:'wanted'"`
	Monitored bool   `gorm:"default:true"`

	JobID           *uint64 `gorm:"index"`
	AcquiredDate    *time.Time
	FilePath        string
	AcquiredFormat  string
	AcquiredBitrate int

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (m *TrackedRelease) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// Library represents a collection of music files
type Library struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string    `gorm:"not null" json:"name"`
	Path         string    `gorm:"uniqueIndex;not null" json:"path"`
	MaxSizeBytes *int64    `gorm:"default:null" json:"max_size_bytes,omitempty"` // nil = no limit
	QuotaAlertAt *int      `gorm:"default:80" json:"quota_alert_at,omitempty"`   // percentage threshold for alerts
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	OwnerUserID  *uint64 `gorm:"index" json:"owner_user_id,omitempty"`
}

func (m *Library) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// Track represents a single audio file in the library
type Track struct {
	ID                   uuid.UUID `gorm:"type:uuid;primaryKey"`
	LibraryID            uuid.UUID `gorm:"type:uuid;not null;index:idx_library_genre"`
	Title                string    `gorm:"not null"`
	Artist               string    `gorm:"index"`
	Album                string    `gorm:"index"`
	Path                 string    `gorm:"uniqueIndex;not null"`
	TrackNum             *int
	DiscNum              *int
	Format               string
	FileSize             int64
	FileHash             string `gorm:"index"`
	Year                 *int   // Release year
	Genre                string `gorm:"index:idx_library_genre"` // Genre
	Composer             string // Composer
	CoverURL             string // URL to cover art
	Fingerprint          string // AcoustID fingerprint (stored after first scan)
	EnrichmentProvenance string `gorm:"type:text"` // JSON: which source wrote which tag fields
	CreatedAt            time.Time
	UpdatedAt            time.Time

	Library Library `gorm:"foreignKey:LibraryID"`
}

func (m *Track) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// Watchlist represents an automated monitoring source (Spotify playlist/Liked Songs)
type Watchlist struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name             string    `gorm:"not null"`
	SourceType       string    `gorm:"not null"` // e.g., "spotify_playlist", "spotify_liked"
	SourceURI        string    `gorm:"uniqueIndex;not null"`
	QualityProfileID uuid.UUID `gorm:"type:uuid;not null;index"`
	LastSnapshotID   string    // Used for Spotify delta checks
	LastSyncedAt     *time.Time
	Enabled          bool `gorm:"default:true"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	OwnerUserID      *uint64 `gorm:"index"`

	QualityProfile QualityProfile `gorm:"foreignKey:QualityProfileID"`
}

func (m *Watchlist) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// SpotifyToken stores User OAuth tokens
type SpotifyToken struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	UserID       uint64 `gorm:"uniqueIndex;not null"`
	AccessToken  string `gorm:"not null"`
	RefreshToken string `gorm:"not null"`
	TokenType    string
	Expiry       time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time

	User User `gorm:"foreignKey:UserID"`
}

// Schedule represents a recurring sync schedule
type Schedule struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"`
	WatchlistID uuid.UUID  `gorm:"type:uuid;not null;index"`
	CronExpr    string     `gorm:"not null"`
	Timezone    string     `gorm:"not null;default:'UTC'"`
	NextRunAt   *time.Time `gorm:"index"`
	LastRunAt   *time.Time
	Enabled     bool `gorm:"not null;default:true;index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time

	Watchlist Watchlist `gorm:"foreignKey:WatchlistID"`
}

// Job represents a background job
type Job struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	Type        string    `gorm:"column:job_type;not null;index"`
	State       string    `gorm:"column:state;not null;index;default:'queued'"`
	RequestedAt time.Time `gorm:"index"`
	StartedAt   *time.Time
	FinishedAt  *time.Time
	HeartbeatAt *time.Time
	WorkerID    *string
	Attempt     int `gorm:"default:0"`
	MaxAttempts int `gorm:"default:3"`
	ScopeType   string
	ScopeID     string
	Params      json.RawMessage `gorm:"type:jsonb"`
	Summary     string
	ErrorDetail string  `gorm:"column:error_detail"`
	CreatedBy   string  `gorm:"column:created_by"`
	OwnerUserID *uint64 `gorm:"index"`
}

func (m *Job) BeforeCreate(tx *gorm.DB) error {
	if m.RequestedAt.IsZero() {
		m.RequestedAt = time.Now()
	}
	return nil
}

// JobLog represents a log entry for a job
type JobLog struct {
	ID        uint64  `gorm:"primaryKey;autoIncrement"`
	JobID     uint64  `gorm:"not null;index"`
	JobItemID *uint64 `gorm:"index"`
	Level     string  `gorm:"not null"`
	Message   string  `gorm:"not null"`
	CreatedAt time.Time
}

func (m *JobLog) BeforeCreate(tx *gorm.DB) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return nil
}

// JobItem represents a unit of work within a job
type JobItem struct {
	ID              uint64 `gorm:"primaryKey;autoIncrement"`
	JobID           uint64 `gorm:"not null;index"`
	Status          string `gorm:"default:'queued'"`
	NormalizedQuery string `gorm:"not null"`
	Artist          string
	Album           string
	TrackTitle      string
	SlskdSearchID   string
	SlskdDownloadID string
	DownloadPath    string
	FinalPath       string
	StartedAt       *time.Time
	FinishedAt      *time.Time
	FailureReason   string
	RetryCount      int        `gorm:"default:0"`
	NextAttemptAt   *time.Time `gorm:"index"`
	CoverArtURL     string
	Sequence        int     `gorm:"not null"`
	OwnerUserID     *uint64 `gorm:"index"`
}

// Acquisition represents an imported item
type Acquisition struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	JobID        uint64 `gorm:"not null;index"`
	JobItemID    uint64 `gorm:"not null;index"`
	Artist       string `gorm:"not null"`
	Album        string
	TrackTitle   string `gorm:"not null"`
	OriginalPath string `gorm:"not null"`
	FinalPath    string `gorm:"not null;index"`
	FileSize     int64
	FileHash     string
	AcquiredAt   time.Time
	ImportedAt   time.Time
	SourceUser   string
	SourceIP     string
	OwnerUserID  *uint64 `gorm:"index"`

	// MB Fields
	MBRecordingID string `gorm:"column:mb_recording_id"`
	MBReleaseID   string `gorm:"column:mb_release_id"`
	MBArtistID    string `gorm:"column:mb_artist_id"`

	// AcoustID
	AcoustIDScore int `gorm:"column:acoustid_score"` // 0-100 confidence score from AcoustID lookup
}

func (m *Acquisition) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	if m.AcquiredAt.IsZero() {
		m.AcquiredAt = now
	}
	if m.ImportedAt.IsZero() {
		m.ImportedAt = now
	}
	return nil
}

// MetadataCache stores cached API responses
type MetadataCache struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	Source    string    `gorm:"index:idx_cache_lookup,priority:1;not null"` // e.g., "musicbrainz", "spotify"
	Key       string    `gorm:"index:idx_cache_lookup,priority:2;not null"` // e.g., artist ID, search query hash
	Value     []byte    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
}

func (m *MetadataCache) BeforeCreate(tx *gorm.DB) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return nil
}

// Lock represents a distributed lock
type Lock struct {
	Key       int64     `gorm:"primaryKey"`
	ExpiresAt time.Time `gorm:"index"`
}

// Setting represents a global application setting
type Setting struct {
	Key       string `gorm:"primaryKey"`
	Value     string
	Type      string `gorm:"default:'string'"`
	UpdatedAt time.Time
}

// PeerReputation tracks Soulseek peer reliability for scoring adjustments.
type PeerReputation struct {
	Username       string `gorm:"primaryKey"`
	TotalDownloads int    `gorm:"default:0"`
	SuccessfulDls  int    `gorm:"default:0"`
	FailedDls      int    `gorm:"default:0"`
	AvgSpeed       int    `gorm:"default:0"` // bytes/sec
	LastSeen       time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SuccessRate returns the download success rate as a float [0, 1].
func (p *PeerReputation) SuccessRate() float64 {
	if p.TotalDownloads == 0 {
		return 1.0
	}
	return float64(p.SuccessfulDls) / float64(p.TotalDownloads)
}

// IsIgnored returns true if the peer should be ignored (success rate < 20% with enough data).
func (p *PeerReputation) IsIgnored() bool {
	return p.TotalDownloads >= 5 && p.SuccessRate() < 0.2
}

// TableName overrides for GORM
func (Job) TableName() string             { return "jobs" }
func (JobItem) TableName() string         { return "jobitems" }
func (Acquisition) TableName() string     { return "acquisitions" }
func (User) TableName() string            { return "users" }
func (Session) TableName() string         { return "sessions" }
func (QualityProfile) TableName() string  { return "quality_profiles" }
func (MonitoredArtist) TableName() string { return "monitored_artists" }
func (TrackedRelease) TableName() string  { return "tracked_releases" }
func (Lock) TableName() string            { return "locks" }
func (Setting) TableName() string         { return "settings" }
func (PeerReputation) TableName() string  { return "peer_reputations" }
