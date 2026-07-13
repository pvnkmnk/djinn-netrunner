package api

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SubsonicHandler handles Subsonic API endpoints
type SubsonicHandler struct {
	db           *gorm.DB
	cfg          *config.Config
	md5Password  string // hex(md5(SubsonicPassword)) for token verification
}

// NewSubsonicHandler creates a new SubsonicHandler
func NewSubsonicHandler(db *gorm.DB, cfg *config.Config) *SubsonicHandler {
	h := &SubsonicHandler{db: db, cfg: cfg}
	if cfg.Subsonic.Password != "" {
		// Compute md5 hash of the password for token verification
		md5Hash := md5.Sum([]byte(cfg.Subsonic.Password))
		h.md5Password = hex.EncodeToString(md5Hash[:])
	}
	return h
}

// Subsonic response types

type subsonicResponse struct {
	Status          string           `xml:"status,attr" json:"status"`
	Version         string           `xml:"version,attr" json:"version"`
	Type            string           `xml:"type,attr" json:"type"`
	Error           *subsonicError   `xml:"error,omitempty" json:"error,omitempty"`
	MusicDirectory  *musicDirectory  `xml:"musicDirectory,omitempty" json:"musicDirectory,omitempty"`
	Song            *subsonicSong    `xml:"song,omitempty" json:"song,omitempty"`
	Album           *subsonicAlbum   `xml:"album,omitempty" json:"album,omitempty"`
	Indexes         *subsonicIndexes `xml:"indexes,omitempty" json:"indexes,omitempty"`
	License         *subsonicLicense `xml:"license,omitempty" json:"license,omitempty"`
	SearchResult3   *searchResult3   `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	AlbumList2      *albumList2      `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	RandomSongs     *randomSongs     `xml:"randomSongs,omitempty" json:"randomSongs,omitempty"`
	ScanStatus      *scanStatus      `xml:"scanStatus,omitempty" json:"scanStatus,omitempty"`
	Playlists       *subsonicPlaylists `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist        *subsonicPlaylist `xml:"playlist,omitempty" json:"playlist,omitempty"`
}

// respond formats and sends a Subsonic response as XML or JSON based on the f parameter
func (h *SubsonicHandler) respond(c *fiber.Ctx, resp *subsonicResponse) error {
	if c.Query("f") == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
}

type subsonicError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type subsonicSong struct {
	ID        string `xml:"id,attr" json:"id"`
	Title     string `xml:"title,attr" json:"title"`
	Artist    string `xml:"artist,attr" json:"artist"`
	Album     string `xml:"album,attr" json:"album"`
	Path      string `xml:"path,attr" json:"path"`
	Track     int    `xml:"track,attr" json:"track,omitempty"`
	Year      int    `xml:"year,attr" json:"year,omitempty"`
	Genre     string `xml:"genre,attr" json:"genre,omitempty"`
	Size      int64  `xml:"size,attr" json:"size,omitempty"`
	Format    string `xml:"contentType,attr" json:"contentType,omitempty"`
	Duration  int    `xml:"duration,attr" json:"duration,omitempty"`
	ArtistID  string `xml:"artistId,attr" json:"artistId,omitempty"`
	AlbumID   string `xml:"albumId,attr" json:"albumId,omitempty"`
	CoverArt  string `xml:"coverArt,attr" json:"coverArt,omitempty"`
	IsDir     bool   `xml:"isDir,attr" json:"isDir,omitempty"`
}

type musicDirectory struct {
	ID    string         `xml:"id,attr" json:"id"`
	Name  string         `xml:"name,attr" json:"name"`
	Child []subsonicSong `xml:"child" json:"child,omitempty"`
}

type subsonicAlbum struct {
	ID        string `xml:"id,attr" json:"id"`
	Name      string `xml:"name,attr" json:"name"`
	Artist    string `xml:"artist,attr" json:"artist"`
	ArtistID  string `xml:"artistId,attr" json:"artistId,omitempty"`
	SongCount int    `xml:"songCount,attr" json:"songCount"`
	Year      int    `xml:"year,attr" json:"year,omitempty"`
	Genre     string `xml:"genre,attr" json:"genre,omitempty"`
	CoverArt  string `xml:"coverArt,attr" json:"coverArt,omitempty"`
	Duration  int    `xml:"duration,attr" json:"duration,omitempty"`
}

type subsonicIndexes struct {
	LastModified int64           `xml:"lastModified,attr" json:"lastModified"`
	Index        []subsonicIndex `xml:"index,omitempty" json:"index,omitempty"`
}

type subsonicIndex struct {
	Name   string           `xml:"name,attr" json:"name"`
	Artist []subsonicArtist `xml:"artist,omitempty" json:"artist,omitempty"`
}

type subsonicArtist struct {
	ID         string `xml:"id,attr" json:"id"`
	Name       string `xml:"name,attr" json:"name"`
	AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
}

type subsonicLicense struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

type searchResult3 struct {
	Artist []subsonicArtist `xml:"artist,omitempty" json:"artist,omitempty"`
	Album  []subsonicAlbum  `xml:"album,omitempty" json:"album,omitempty"`
	Song   []subsonicSong   `xml:"song,omitempty" json:"song,omitempty"`
}

type albumList2 struct {
	Album []subsonicAlbum `xml:"album,omitempty" json:"album,omitempty"`
}

type randomSongs struct {
	Song []subsonicSong `xml:"song,omitempty" json:"song,omitempty"`
}

type scanStatus struct {
	Scanning bool `xml:"scanning,attr" json:"scanning"`
	Count    int  `xml:"count,attr" json:"count"`
}

type subsonicPlaylists struct {
	XMLName   xml.Name          `xml:"playlists" json:"-"`
	Playlists []subsonicPlaylist `xml:"playlist" json:"playlist"`
}

type subsonicPlaylist struct {
	XMLName   xml.Name        `xml:"playlist" json:"-"`
	ID        string          `xml:"id,attr" json:"id"`
	Name      string          `xml:"name,attr" json:"name"`
	Comment   string          `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Owner     string          `xml:"owner,attr,omitempty" json:"owner,omitempty"`
	Public    bool            `xml:"public,attr,omitempty" json:"public,omitempty"`
	SongCount int             `xml:"songCount,attr" json:"songCount"`
	Duration  int             `xml:"duration,attr" json:"duration"`
	Created   string          `xml:"created,attr" json:"created"`
	Changed   string          `xml:"changed,attr" json:"changed"`
	Entries   []subsonicChild `xml:"entry,omitempty" json:"entry,omitempty"`
}

type subsonicChild struct {
	XMLName   xml.Name `xml:"child" json:"-"`
	ID        string   `xml:"id,attr" json:"id"`
	Title     string   `xml:"title,attr" json:"title"`
	Artist    string   `xml:"artist,attr" json:"artist"`
	Album     string   `xml:"album,attr" json:"album"`
	Path      string   `xml:"path,attr" json:"path"`
	Track     int      `xml:"track,attr" json:"track,omitempty"`
	Year      int      `xml:"year,attr" json:"year,omitempty"`
	Genre     string   `xml:"genre,attr" json:"genre,omitempty"`
	Size      int64    `xml:"size,attr" json:"size,omitempty"`
	Format    string   `xml:"contentType,attr" json:"contentType,omitempty"`
	Duration  int      `xml:"duration,attr" json:"duration,omitempty"`
	ArtistID  string   `xml:"artistId,attr" json:"artistId,omitempty"`
	AlbumID   string   `xml:"albumId,attr" json:"albumId,omitempty"`
	CoverArt  string   `xml:"coverArt,attr" json:"coverArt,omitempty"`
	IsDir     bool     `xml:"isDir,attr" json:"isDir,omitempty"`
}

// AuthMiddleware validates Subsonic authentication parameters
func (h *SubsonicHandler) AuthMiddleware(c *fiber.Ctx) error {
	// Parse query parameters
	username := c.Query("u")
	token := c.Query("t")
	salt := c.Query("s")
	password := c.Query("p")

	if username == "" {
		return h.respondError(c, 40, "Missing parameter: u")
	}

	// Look up user by email
	var user database.User
	if err := h.db.Where("LOWER(email) = ?", strings.ToLower(username)).First(&user).Error; err != nil {
		slog.Warn("Subsonic auth failed: user not found", "username", username)
		return h.respondError(c, 40, "Authentication failed")
	}

	// Authentication check
	if password != "" {
		// Password authentication
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			slog.Warn("Subsonic auth failed: invalid password", "username", username)
			return h.respondError(c, 40, "Authentication failed")
		}
	} else if token != "" && salt != "" {
		// Token authentication
		hash := md5.Sum([]byte(h.md5Password + salt))
		expected := hex.EncodeToString(hash[:])
		if token != expected {
			slog.Warn("Subsonic auth failed: invalid token", "username", username)
			return h.respondError(c, 40, "Authentication failed")
		}
	} else {
		return h.respondError(c, 40, "Missing authentication parameters")
	}

	// Set user in context
	c.Locals("user", user)
	return c.Next()
}

// respondXML returns an XML response
func (h *SubsonicHandler) respondXML(c *fiber.Ctx, resp interface{}) error {
	c.Set("Content-Type", "application/xml; charset=utf-8")
	return c.XML(resp)
}

// respondJSON returns a JSON response wrapped in subsonic-response envelope
func (h *SubsonicHandler) respondJSON(c *fiber.Ctx, resp interface{}) error {
	c.Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(fiber.Map{"subsonic-response": resp})
}

// respondError returns a Subsonic error response
func (h *SubsonicHandler) respondError(c *fiber.Ctx, code int, message string) error {
	error := &subsonicError{Code: code, Message: message}
	resp := &subsonicResponse{
		Status:  "failed",
		Version: "1.16.1",
		Type:    "netrunner",
		Error:   error,
	}

	return h.respond(c, resp)
}

// Ping handles the ping endpoint
func (h *SubsonicHandler) Ping(c *fiber.Ctx) error {
	resp := &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
	}

	return h.respond(c, resp)
}

// License handles the license endpoint
func (h *SubsonicHandler) License(c *fiber.Ctx) error {
	resp := &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		License: &subsonicLicense{Valid: true},
	}

	return h.respond(c, resp)
}

// GetIndexes handles the getIndexes endpoint
func (h *SubsonicHandler) GetIndexes(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	// Query distinct artists for the user's libraries
	var artists []struct{ Name string }
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ?", user.ID).
		Select("DISTINCT artist").
		Order("artist").
		Find(&artists)

	// Group by first letter
	indexes := make([]subsonicIndex, 0)
	lastModified := time.Now().UnixMilli()

	for _, artist := range artists {
		if artist.Name == "" {
			continue
		}
		firstChar := string(artist.Name[0])
		if !strings.ContainsAny(firstChar, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			firstChar = "#"
		}

		// Check if we already have an index for this letter
		found := false
		for i, idx := range indexes {
			if idx.Name == firstChar {
				indexes[i].Artist = append(indexes[i].Artist, subsonicArtist{ID: "", Name: artist.Name, AlbumCount: 0})
				found = true
				break
			}
		}

		if !found {
			index := subsonicIndex{Name: firstChar}
			index.Artist = append(index.Artist, subsonicArtist{ID: "", Name: artist.Name, AlbumCount: 0})
			indexes = append(indexes, index)
		}
	}

	// Get album counts for each artist
	for i := range indexes {
		for j := range indexes[i].Artist {
			var count int64
			h.db.Table("tracks").
				Joins("JOIN libraries ON libraries.id = tracks.library_id").
				Where("libraries.owner_user_id = ? AND artist = ?", user.ID, indexes[i].Artist[j].Name).
				Count(&count)
			indexes[i].Artist[j].AlbumCount = int(count)
		}
	}

	resp := &subsonicResponse{
		Status:     "ok",
		Version:    "1.16.1",
		Type:       "netrunner",
		Indexes:    &subsonicIndexes{LastModified: lastModified, Index: indexes},
	}

	return h.respond(c, resp)
}

// GetMusicDirectory handles the getMusicDirectory endpoint
func (h *SubsonicHandler) GetMusicDirectory(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	// Parse ID
	var directory musicDirectory
	var err error

	if strings.HasPrefix(id, "artist-") {
		// Artist directory
		encodedName := strings.TrimPrefix(id, "artist-")
		name, err := url.PathUnescape(encodedName)
		if err != nil {
			return h.respondError(c, 10, "Invalid artist ID")
		}
		directory, err = h.getArtistDirectory(user, name)
	} else if strings.HasPrefix(id, "album-") {
		// Album directory
		parts := strings.SplitN(strings.TrimPrefix(id, "album-"), "-", 2)
		if len(parts) != 2 {
			return h.respondError(c, 10, "Invalid album ID")
		}
		encodedName := parts[0]
		encodedArtist := parts[1]
		name, err1 := url.PathUnescape(encodedName)
		artist, err2 := url.PathUnescape(encodedArtist)
		if err1 != nil || err2 != nil {
			return h.respondError(c, 10, "Invalid album ID")
		}
		directory, err = h.getAlbumDirectory(user, name, artist)
	} else {
		// Track ID (UUID)
		directory, err = h.getTrackDirectory(user, id)
	}

	if err != nil {
		return h.respondError(c, 70, err.Error())
	}

	resp := &subsonicResponse{
		Status:      "ok",
		Version:     "1.16.1",
		Type:        "netrunner",
		MusicDirectory: &directory,
	}

	return h.respond(c, resp)
}

// getArtistDirectory returns a directory for an artist
func (h *SubsonicHandler) getArtistDirectory(user database.User, artistName string) (musicDirectory, error) {
	var albums []struct {
		ID    string
		Name  string
		Artist string
	}

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND artist = ?", user.ID, artistName).
		Select("DISTINCT album, artist").
		Order("album").
		Find(&albums)

	if len(albums) == 0 {
		return musicDirectory{}, fmt.Errorf("artist not found")
	}

	// Create directory entry
	directory := musicDirectory{
		ID:   "artist-" + url.PathEscape(artistName),
		Name: artistName,
	}

	// Add albums as children
	for _, album := range albums {
		child := subsonicSong{
			ID:        "album-" + url.PathEscape(album.Name) + "-" + url.PathEscape(album.Artist),
			Title:     album.Name,
			Artist:    album.Artist,
			Album:     album.Name,
			IsDir:     true,
		}
		directory.Child = append(directory.Child, child)
	}

	return directory, nil
}

// getAlbumDirectory returns a directory for an album
func (h *SubsonicHandler) getAlbumDirectory(user database.User, albumName, artistName string) (musicDirectory, error) {
	var tracks []database.Track

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, albumName, artistName).
		Order("track_num").
		Find(&tracks)

	if len(tracks) == 0 {
		return musicDirectory{}, fmt.Errorf("album not found")
	}

	// Create directory entry
	directory := musicDirectory{
		ID:   "album-" + url.PathEscape(albumName) + "-" + url.PathEscape(artistName),
		Name: albumName,
	}

	// Add tracks as children
	for _, track := range tracks {
		child := subsonicSong{
			ID:        track.ID.String(),
			Title:     track.Title,
			Artist:    track.Artist,
			Album:     track.Album,
			Path:      track.Path,
			Track:     safeDeref(track.TrackNum),
			Year:      safeDeref(track.Year),
			Genre:     track.Genre,
			Size:      track.FileSize,
			Format:    track.Format,
			Duration:  h.getTrackDuration(track.Path),
			ArtistID:  "",
			AlbumID:   "",
			CoverArt:  track.CoverURL,
		}
		directory.Child = append(directory.Child, child)
	}

	return directory, nil
}

// getTrackDirectory returns a directory for a single track
func (h *SubsonicHandler) getTrackDirectory(user database.User, trackID string) (musicDirectory, error) {
	var track database.Track

	// Parse UUID
	parsedID, err := uuid.Parse(trackID)
	if err != nil {
		return musicDirectory{}, fmt.Errorf("invalid track ID")
	}

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND tracks.id = ?", user.ID, parsedID).
		First(&track)

	if track.ID == (uuid.UUID{}) {
		return musicDirectory{}, fmt.Errorf("track not found")
	}

	// Create directory entry
	directory := musicDirectory{
		ID:   trackID,
		Name: track.Title,
	}

	// Add track as child
	child := subsonicSong{
		ID:        track.ID.String(),
		Title:     track.Title,
		Artist:    track.Artist,
		Album:     track.Album,
		Path:      track.Path,
		Track:     safeDeref(track.TrackNum),
		Year:      safeDeref(track.Year),
		Genre:     track.Genre,
		Size:      track.FileSize,
		Format:    track.Format,
		Duration:  h.getTrackDuration(track.Path),
		ArtistID:  "",
		AlbumID:   "",
		CoverArt:  track.CoverURL,
	}
	directory.Child = append(directory.Child, child)

	return directory, nil
}

// safeDeref safely dereferences an *int, returning 0 if nil
func safeDeref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// getTrackDuration returns the duration of a track in seconds
func (h *SubsonicHandler) getTrackDuration(path string) int {
	// For now, return 0 as a placeholder
	// In a real implementation, this would use a media library to get the duration
	return 0
}

// GetSong handles the getSong endpoint
func (h *SubsonicHandler) GetSong(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	var track database.Track
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return h.respondError(c, 10, "Invalid track ID")
	}

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND tracks.id = ?", user.ID, parsedID).
		First(&track)

	if track.ID == (uuid.UUID{}) {
		return h.respondError(c, 70, "Track not found")
	}

	// Create song response
	song := &subsonicSong{
		ID:        track.ID.String(),
		Title:     track.Title,
		Artist:    track.Artist,
		Album:     track.Album,
		Path:      track.Path,
		Track:     safeDeref(track.TrackNum),
		Year:      safeDeref(track.Year),
		Genre:     track.Genre,
		Size:      track.FileSize,
		Format:    track.Format,
		Duration:  h.getTrackDuration(track.Path),
		ArtistID:  "",
		AlbumID:   "",
		CoverArt:  track.CoverURL,
	}

	resp := &subsonicResponse{
		Status: "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Song:    song,
	}

	return h.respond(c, resp)
}

// GetAlbum handles the getAlbum endpoint
func (h *SubsonicHandler) GetAlbum(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	// Parse album ID (format: album-{name}-{artist})
	if !strings.HasPrefix(id, "album-") {
		return h.respondError(c, 10, "Invalid album ID format")
	}

	parts := strings.SplitN(strings.TrimPrefix(id, "album-"), "-", 2)
	if len(parts) != 2 {
		return h.respondError(c, 10, "Invalid album ID")
	}

	albumName, err1 := url.PathUnescape(parts[0])
	artistName, err2 := url.PathUnescape(parts[1])
	if err1 != nil || err2 != nil {
		return h.respondError(c, 10, "Invalid album ID")
	}

	var tracks []database.Track
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, albumName, artistName).
		Order("track_num").
		Find(&tracks)

	if len(tracks) == 0 {
		return h.respondError(c, 70, "Album not found")
	}

	// Calculate total duration
	totalDuration := 0
	for _, track := range tracks {
		totalDuration += h.getTrackDuration(track.Path)
	}

	// Create album response
	album := &subsonicAlbum{
		ID:        id,
		Name:      albumName,
		Artist:    artistName,
		ArtistID:  "",
		SongCount: len(tracks),
		Year:      safeDeref(tracks[0].Year),
		Genre:     tracks[0].Genre,
		CoverArt:  tracks[0].CoverURL,
		Duration:  totalDuration,
	}

	resp := &subsonicResponse{
		Status: "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Album:   album,
	}

	return h.respond(c, resp)
}

// GetArtist handles the getArtist endpoint
func (h *SubsonicHandler) GetArtist(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	// Parse artist ID (format: artist-{name})
	if !strings.HasPrefix(id, "artist-") {
		return h.respondError(c, 10, "Invalid artist ID format")
	}

	encodedName := strings.TrimPrefix(id, "artist-")
	artistName, err := url.PathUnescape(encodedName)
	if err != nil {
		return h.respondError(c, 10, "Invalid artist ID")
	}

	var tracks []database.Track

h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND artist = ?", user.ID, artistName).
		Order("album").
		Find(&tracks)

	if len(tracks) == 0 {
		return h.respondError(c, 70, "Artist not found")
	}

	// Get unique albums
	albums := make(map[string]bool)
	for _, track := range tracks {
		albums[track.Album] = true
	}

	// Create artist response
	artist := &subsonicArtist{
		ID:         id,
		Name:       artistName,
		AlbumCount: len(albums),
	}

	resp := &subsonicResponse{
		Status: "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Indexes: &subsonicIndexes{Index: []subsonicIndex{{Name: "A", Artist: []subsonicArtist{*artist}}}},
	}

	return h.respond(c, resp)
}

// Stream handles the stream endpoint
func (h *SubsonicHandler) Stream(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	var track database.Track
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return h.respondError(c, 10, "Invalid track ID")
	}

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND tracks.id = ?", user.ID, parsedID).
		First(&track)

	if track.ID == (uuid.UUID{}) {
		return h.respondError(c, 70, "Track not found")
	}

	if _, err := os.Stat(track.Path); err != nil {
		return h.respondError(c, 70, "File not found")
	}

	// Determine requested format
	requestedFormat := c.Query("format")
	if requestedFormat == "" {
		// Try Accept header negotiation
		requestedFormat = negotiateFormat(c.Get("Accept"))
	}

	// Normalize format to lowercase
	requestedFormat = strings.ToLower(requestedFormat)

	// Get source format (stored uppercase in DB)
	sourceFormat := strings.ToLower(track.Format)

	// Determine if transcoding is needed
	needsTranscode := false
	if requestedFormat != "" && requestedFormat != sourceFormat && h.cfg.Transcode.Enabled {
		needsTranscode = true
	}

	if needsTranscode {
		// Parse bitrate
		bitrate := 0
		if b := c.Query("bitrate"); b != "" {
			if parsed, err := strconv.Atoi(b); err == nil && parsed > 0 {
				bitrate = parsed
				if h.cfg.Transcode.MaxBitrate > 0 && bitrate > h.cfg.Transcode.MaxBitrate {
					bitrate = h.cfg.Transcode.MaxBitrate
				}
			}
		}

		// Set content type for transcoded output
		contentType := formatToMIME(requestedFormat)
		c.Set("Content-Type", contentType)
		c.Set("Accept-Ranges", "none") // Streaming transcode doesn't support range requests

		// Stream transcoded audio
		return h.streamTranscoded(c, track.Path, requestedFormat, bitrate)
	}

	// Serve original file (existing behavior)
	contentType := formatToMIME(sourceFormat)
	if contentType == "" {
		// Fallback to extension-based detection
		contentType = "application/octet-stream"
		switch {
		case strings.HasSuffix(track.Path, ".mp3"):
			contentType = "audio/mpeg"
		case strings.HasSuffix(track.Path, ".flac"):
			contentType = "audio/flac"
		case strings.HasSuffix(track.Path, ".wav"):
			contentType = "audio/wav"
		case strings.HasSuffix(track.Path, ".ogg"):
			contentType = "audio/ogg"
		case strings.HasSuffix(track.Path, ".m4a"):
			contentType = "audio/m4a"
		}
	}

	c.Set("Content-Type", contentType)
	c.Set("Accept-Ranges", "bytes")
	c.Set("Content-Length", strconv.FormatInt(track.FileSize, 10))

	return c.SendFile(track.Path)
}

// streamTranscoded pipes FFmpeg output to the HTTP response
func (h *SubsonicHandler) streamTranscoded(c *fiber.Ctx, inputPath, outputFormat string, bitrate int) error {
	// Set up streaming response
	c.Set("Transfer-Encoding", "chunked")
	c.Set("Cache-Control", "no-cache")

	// Create a pipe to connect FFmpeg stdout to HTTP response
	pr, pw := io.Pipe()

	// Start FFmpeg in a goroutine
	go func() {
		defer pw.Close()

		ffmpegPath := h.cfg.Transcode.FFmpegPath
		if ffmpegPath == "" {
			ffmpegPath = "ffmpeg"
		}

		args := []string{"-i", inputPath, "-f", outputFormat}
		if bitrate > 0 && isLossyFormat(outputFormat) {
			args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate))
		}
		args = append(args, "-v", "quiet", "pipe:1")

		cmd := exec.Command(ffmpegPath, args...)
		cmd.Stdout = pw

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			pw.CloseWithError(fmt.Errorf("transcode failed: %w: %s", err, stderr.String()))
		}
	}()

	// Stream the pipe reader to the response
	defer pr.Close()
	_, err := io.Copy(c.Response().BodyWriter(), pr)
	return err
}

// negotiateFormat parses the Accept header and returns the best audio format
func negotiateFormat(accept string) string {
	if accept == "" {
		return ""
	}

	// Simple priority-based negotiation
	// Check for common audio MIME types
	acceptLower := strings.ToLower(accept)

	if strings.Contains(acceptLower, "audio/mpeg") || strings.Contains(acceptLower, "audio/mp3") {
		return "mp3"
	}
	if strings.Contains(acceptLower, "audio/ogg") {
		return "ogg"
	}
	if strings.Contains(acceptLower, "audio/opus") {
		return "opus"
	}
	if strings.Contains(acceptLower, "audio/aac") || strings.Contains(acceptLower, "audio/mp4") {
		return "aac"
	}
	if strings.Contains(acceptLower, "audio/flac") {
		return "flac"
	}
	if strings.Contains(acceptLower, "audio/wav") {
		return "wav"
	}

	return ""
}

// formatToMIME converts a format string to its MIME type
func formatToMIME(format string) string {
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "opus":
		return "audio/opus"
	case "aac", "m4a":
		return "audio/mp4"
	default:
		return ""
	}
}

// isLossyFormat returns true if the format is lossy (supports bitrate control)
func isLossyFormat(format string) bool {
	switch strings.ToLower(format) {
	case "mp3", "aac", "ogg", "opus", "m4a":
		return true
	}
	return false
}

// GetCoverArt handles the getCoverArt endpoint
func (h *SubsonicHandler) GetCoverArt(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing parameter: id")
	}

	var track database.Track
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return h.respondError(c, 10, "Invalid track ID")
	}

	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND tracks.id = ?", user.ID, parsedID).
		First(&track)

	if track.ID == (uuid.UUID{}) {
		return h.respondError(c, 70, "Track not found")
	}

	// Try to get cover art from track
	if track.CoverURL != "" {
		// If it's a URL, fetch it
		resp, err := http.Get(track.CoverURL)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				// Read the image data
				imageData, err := io.ReadAll(resp.Body)
				if err == nil {
					c.Set("Content-Type", "image/jpeg")
					return c.Send(imageData)
				}
			}
		}
	}

	// Try to find cover art in the track's directory
	coverPath := findCoverArtInDirectory(track.Path)
	if coverPath != "" {
		if _, err := os.Stat(coverPath); err == nil {
			return c.SendFile(coverPath)
		}
	}

	// Return a placeholder image or error
	return h.respondError(c, 0, "Cover art not available")
}

// findCoverArtInDirectory looks for cover.jpg or folder.jpg in the track's directory
func findCoverArtInDirectory(path string) string {
	dir := filepath.Dir(path)
	for _, name := range []string{"cover.jpg", "folder.jpg", "album.jpg"} {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// Search3 handles the search3 endpoint
func (h *SubsonicHandler) Search3(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	// Parse query parameters
	query := c.Query("query")
	if query == "" {
		return h.respondError(c, 10, "Missing parameter: query")
	}

	artistCount := 20
	if count := c.Query("artistCount"); count != "" {
		if parsed, err := strconv.Atoi(count); err == nil && parsed > 0 {
			artistCount = parsed
		}
	}

	albumCount := 20
	if count := c.Query("albumCount"); count != "" {
		if parsed, err := strconv.Atoi(count); err == nil && parsed > 0 {
			albumCount = parsed
		}
	}

	songCount := 20
	if count := c.Query("songCount"); count != "" {
		if parsed, err := strconv.Atoi(count); err == nil && parsed > 0 {
			songCount = parsed
		}
	}

	// Parse the search query
	q := "%" + strings.ToLower(query) + "%"

	// Search songs (tracks)
	var tracks []database.Track
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND (LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ?)", user.ID, q, q, q).
		Limit(songCount).
		Find(&tracks)

	// Search artists (distinct)
	var artists []struct{ Name string }
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND LOWER(artist) LIKE ?", user.ID, q).
		Select("DISTINCT artist").
		Limit(artistCount).
		Find(&artists)

	// Search albums (distinct album+artist)
	var albums []struct{ Album, Artist string }
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND LOWER(album) LIKE ?", user.ID, q).
		Select("DISTINCT album, artist").
		Limit(albumCount).
		Find(&albums)

	// Build search result
	searchResult := &searchResult3{}

	// Fill artists
	for _, artist := range artists {
		// Count tracks for this artist
		var count int64
		h.db.Table("tracks").
			Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ? AND artist = ?", user.ID, artist.Name).
			Count(&count)

		searchResult.Artist = append(searchResult.Artist, subsonicArtist{
			ID:         "artist-" + url.PathEscape(artist.Name),
			Name:       artist.Name,
			AlbumCount: int(count),
		})
	}

	// Fill albums
	for _, album := range albums {
		// Count songs for this album
		var songCount int64
		h.db.Table("tracks").
			Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, album.Album, album.Artist).
			Count(&songCount)

		// Get year from first track
		var firstTrack database.Track
		h.db.Table("tracks").
			Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, album.Album, album.Artist).
			First(&firstTrack)

		searchResult.Album = append(searchResult.Album, subsonicAlbum{
			ID:        "album-" + url.PathEscape(album.Album) + "-" + url.PathEscape(album.Artist),
			Name:      album.Album,
			Artist:    album.Artist,
			ArtistID:  "",
			SongCount: int(songCount),
			Year:      safeDeref(firstTrack.Year),
			Genre:     firstTrack.Genre,
			CoverArt:  firstTrack.CoverURL,
			Duration:  h.getAlbumDuration(user, album.Album, album.Artist),
		})
	}

	// Fill songs
	for _, track := range tracks {
		searchResult.Song = append(searchResult.Song, subsonicSong{
			ID:       track.ID.String(),
			Title:    track.Title,
			Artist:   track.Artist,
			Album:    track.Album,
			Path:     track.Path,
			Track:    safeDeref(track.TrackNum),
			Year:     safeDeref(track.Year),
			Genre:    track.Genre,
			Size:     track.FileSize,
			Format:   track.Format,
			Duration: h.getTrackDuration(track.Path),
			ArtistID: "",
			AlbumID:  "",
			CoverArt: track.CoverURL,
		})
	}

	resp := &subsonicResponse{
		Status:     "ok",
		Version:    "1.16.1",
		Type:       "netrunner",
		SearchResult3: searchResult,
	}

	return h.respond(c, resp)
}

// GetAlbumList2 handles the getAlbumList2 endpoint
func (h *SubsonicHandler) GetAlbumList2(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	// Parse query parameters
	listType := c.Query("type")
	if listType == "" {
		listType = "random"
	}

	size := 50
	if s := c.Query("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 {
			size = parsed
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Query distinct album+artist combinations from tracks
	type albumRow struct {
		Album  string
		Artist string
		Year   *int
		Genre  string
	}

	var rows []albumRow

	tx := h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ?", user.ID).
		Select("album, artist, MAX(year) as year").
		Group("album, artist")

	switch listType {
	case "random":
		tx = tx.Order("RANDOM()")
	case "newest":
		tx = tx.Order("COALESCE(MAX(year), 0) DESC")
	case "alphabeticalByName":
		tx = tx.Order("album")
	case "alphabeticalByArtist":
		tx = tx.Order("artist, album")
	default:
		tx = tx.Order("album")
	}

	tx.Offset(offset).Limit(size).Find(&rows)

	// Build album list result
	albumList := &albumList2{}

	for _, row := range rows {
		// Get song count and total duration
		var songCount int64
		h.db.Table("tracks").
			Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, row.Album, row.Artist).
			Count(&songCount)

		var totalDuration int
		var tracks []database.Track
		h.db.Table("tracks").
			Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, row.Album, row.Artist).
			Find(&tracks)

		for _, track := range tracks {
			totalDuration += h.getTrackDuration(track.Path)
		}

		albumList.Album = append(albumList.Album, subsonicAlbum{
			ID:        "album-" + url.PathEscape(row.Album) + "-" + url.PathEscape(row.Artist),
			Name:      row.Album,
			Artist:    row.Artist,
			ArtistID:  "",
			SongCount: int(songCount),
			Year:      safeDeref(row.Year),
			Genre:     row.Genre,
			CoverArt:  "", // Would need to get from first track
			Duration:  totalDuration,
		})
	}

	resp := &subsonicResponse{
		Status:     "ok",
		Version:    "1.16.1",
		Type:       "netrunner",
		AlbumList2: albumList,
	}

	return h.respond(c, resp)
}

// GetRandomSongs handles the getRandomSongs endpoint
func (h *SubsonicHandler) GetRandomSongs(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Authentication required")
	}

	// Parse query parameters
	size := 10
	if s := c.Query("size"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 {
			size = parsed
		}
	}

	// Query random tracks
	var tracks []database.Track

h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ?", user.ID).
		Order("RANDOM()").
		Limit(size).
		Find(&tracks)

	// Build random songs result
	randomSongs := &randomSongs{}

	for _, track := range tracks {
		randomSongs.Song = append(randomSongs.Song, subsonicSong{
			ID:       track.ID.String(),
			Title:    track.Title,
			Artist:   track.Artist,
			Album:    track.Album,
			Path:     track.Path,
			Track:    safeDeref(track.TrackNum),
			Year:     safeDeref(track.Year),
			Genre:    track.Genre,
			Size:     track.FileSize,
			Format:   track.Format,
			Duration: h.getTrackDuration(track.Path),
			ArtistID: "",
			AlbumID:  "",
			CoverArt: track.CoverURL,
		})
	}

	resp := &subsonicResponse{
		Status:      "ok",
		Version:     "1.16.1",
		Type:        "netrunner",
		RandomSongs: randomSongs,
	}

	return h.respond(c, resp)
}

// GetScanStatus handles the getScanStatus endpoint
func (h *SubsonicHandler) GetScanStatus(c *fiber.Ctx) error {
	// For now, return a scan status indicating scanning is not running
	// In a real implementation, this would check for scan jobs
	scanStatus := &scanStatus{
		Scanning: false,
		Count:    0,
	}

	resp := &subsonicResponse{
		Status:   "ok",
		Version:  "1.16.1",
		Type:     "netrunner",
		ScanStatus: scanStatus,
	}

	return h.respond(c, resp)
}

// StartScan handles the startScan endpoint
func (h *SubsonicHandler) StartScan(c *fiber.Ctx) error {
	// For now, just return a scan status indicating scanning is not running
	// In a real implementation, this would trigger a scan job
	return h.GetScanStatus(c)
}

// getAlbumDuration calculates the total duration of an album
func (h *SubsonicHandler) getAlbumDuration(user database.User, albumName, artistName string) int {
	var tracks []database.Track
	h.db.Table("tracks").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("libraries.owner_user_id = ? AND album = ? AND artist = ?", user.ID, albumName, artistName).
		Find(&tracks)

	totalDuration := 0
	for _, track := range tracks {
		totalDuration += h.getTrackDuration(track.Path)
	}
	return totalDuration
}

// GetPlaylists handles the getPlaylists endpoint
func (h *SubsonicHandler) GetPlaylists(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Not authenticated")
	}

	var playlists []database.Playlist
	query := h.db.Order("name ASC")
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ? OR public = ?", user.ID, true)
	}
	if err := query.Find(&playlists).Error; err != nil {
		return h.respondError(c, 0, "Failed to fetch playlists")
	}

	// For each playlist, count tracks
	var result []subsonicPlaylist
	for _, p := range playlists {
		var count int64
		h.db.Model(&database.PlaylistTrack{}).Where("playlist_id = ?", p.ID).Count(&count)

		// Get owner name
		var ownerName string
		if p.OwnerUserID != nil {
			var owner database.User
			h.db.First(&owner, *p.OwnerUserID)
			ownerName = owner.Email
		}

		result = append(result, subsonicPlaylist{
			ID:        p.ID.String(),
			Name:      p.Name,
			Comment:   p.Description,
			Owner:     ownerName,
			Public:    p.Public,
			SongCount: int(count),
			Duration:  0, // Track has no duration field
			Created:   p.CreatedAt.Format(time.RFC3339),
			Changed:   p.UpdatedAt.Format(time.RFC3339),
		})
	}

	return h.respond(c, &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Playlists: &subsonicPlaylists{Playlists: result},
	})
}

// GetPlaylist handles the getPlaylist endpoint
func (h *SubsonicHandler) GetPlaylist(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Not authenticated")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing playlist id")
	}

	playlistUUID, err := uuid.Parse(id)
	if err != nil {
		return h.respondError(c, 10, "Invalid playlist id")
	}

	var playlist database.Playlist
	if err := h.db.First(&playlist, "id = ?", playlistUUID).Error; err != nil {
		return h.respondError(c, 70, "Playlist not found")
	}

	// Check access
	if !playlist.Public && user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return h.respondError(c, 50, "Access denied")
	}

	// Get ordered tracks
	var playlistTracks []database.PlaylistTrack
	h.db.Where("playlist_id = ?", playlist.ID).Order("position ASC").Preload("Track").Find(&playlistTracks)

	var ownerName string
	if playlist.OwnerUserID != nil {
		var owner database.User
		h.db.First(&owner, *playlist.OwnerUserID)
		ownerName = owner.Email
	}

	var entries []subsonicChild
	for _, pt := range playlistTracks {
		entries = append(entries, subsonicChild{
			ID:        pt.Track.ID.String(),
			Title:     pt.Track.Title,
			Artist:    pt.Track.Artist,
			Album:     pt.Track.Album,
			Path:      pt.Track.Path,
			Track:     safeDeref(pt.Track.TrackNum),
			Year:      safeDeref(pt.Track.Year),
			Genre:     pt.Track.Genre,
			Size:      pt.Track.FileSize,
			Format:    pt.Track.Format,
			Duration:  h.getTrackDuration(pt.Track.Path),
			CoverArt:  pt.Track.CoverURL,
		})
	}

	return h.respond(c, &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Playlist: &subsonicPlaylist{
			ID:        playlist.ID.String(),
			Name:      playlist.Name,
			Comment:   playlist.Description,
			Owner:     ownerName,
			Public:    playlist.Public,
			SongCount: len(entries),
			Duration:  0,
			Created:   playlist.CreatedAt.Format(time.RFC3339),
			Changed:   playlist.UpdatedAt.Format(time.RFC3339),
			Entries:   entries,
		},
	})
}

// CreatePlaylist handles the createPlaylist endpoint
func (h *SubsonicHandler) CreatePlaylist(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Not authenticated")
	}

	playlistID := c.Query("playlistId")
	name := c.Query("name")

	if playlistID != "" {
		// Update existing playlist name
		playlistUUID, err := uuid.Parse(playlistID)
		if err != nil {
			return h.respondError(c, 10, "Invalid playlist id")
		}
		var playlist database.Playlist
		if err := h.db.First(&playlist, "id = ?", playlistUUID).Error; err != nil {
			return h.respondError(c, 70, "Playlist not found")
		}
		if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
			return h.respondError(c, 50, "Access denied")
		}
		if name != "" {
			playlist.Name = name
			h.db.Save(&playlist)
		}
		return h.respond(c, &subsonicResponse{
			Status:  "ok",
			Version: "1.16.1",
			Type:    "netrunner",
			Playlist: &subsonicPlaylist{
				ID:   playlist.ID.String(),
				Name: playlist.Name,
			},
		})
	}

	// Create new playlist
	if name == "" {
		return h.respondError(c, 10, "Missing playlist name")
	}

	playlist := database.Playlist{
		Name:        name,
		Description: c.Query("comment", ""),
		Public:      c.Query("public") == "true",
		OwnerUserID: &user.ID,
	}
	if err := h.db.Create(&playlist).Error; err != nil {
		return h.respondError(c, 0, "Failed to create playlist")
	}

	// Add songs if provided
	songIDs := c.Query("songId") // Subsonic allows multiple songId params
	// Note: fiber doesn't easily support repeated query params, so we handle comma-separated
	if songIDs != "" {
		for i, sid := range strings.Split(songIDs, ",") {
			trackUUID, err := uuid.Parse(strings.TrimSpace(sid))
			if err != nil {
				continue
			}
			h.db.Create(&database.PlaylistTrack{
				PlaylistID: playlist.ID,
				TrackID:    trackUUID,
				Position:   i,
			})
		}
	}

	return h.respond(c, &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
		Playlist: &subsonicPlaylist{
			ID:   playlist.ID.String(),
			Name: playlist.Name,
		},
	})
}

// DeletePlaylist handles the deletePlaylist endpoint
func (h *SubsonicHandler) DeletePlaylist(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return h.respondError(c, 40, "Not authenticated")
	}

	id := c.Query("id")
	if id == "" {
		return h.respondError(c, 10, "Missing playlist id")
	}

	playlistUUID, err := uuid.Parse(id)
	if err != nil {
		return h.respondError(c, 10, "Invalid playlist id")
	}

	var playlist database.Playlist
	if err := h.db.First(&playlist, "id = ?", playlistUUID).Error; err != nil {
		return h.respondError(c, 70, "Playlist not found")
	}

	if user.Role != "admin" && (playlist.OwnerUserID == nil || *playlist.OwnerUserID != user.ID) {
		return h.respondError(c, 50, "Access denied")
	}

	// Delete playlist tracks first, then playlist
	h.db.Where("playlist_id = ?", playlistUUID).Delete(&database.PlaylistTrack{})
	h.db.Delete(&playlist)

	return h.respond(c, &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
	})
}