package api

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
}

// Ping handles the ping endpoint
func (h *SubsonicHandler) Ping(c *fiber.Ctx) error {
	resp := &subsonicResponse{
		Status:  "ok",
		Version: "1.16.1",
		Type:    "netrunner",
	}

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
}

// License handles the license endpoint
func (h *SubsonicHandler) License(c *fiber.Ctx) error {
	license := &subsonicLicense{Valid: true}
	resp := &subsonicResponse{
		Status:    "ok",
		Version:   "1.16.1",
		Type:      "netrunner",
		License:   license,
	}

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	format := c.Query("f", "xml")
	if format == "json" {
		return h.respondJSON(c, resp)
	}
	return h.respondXML(c, resp)
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

	// Check if file exists
	if _, err := os.Stat(track.Path); err != nil {
		return h.respondError(c, 70, "File not found")
	}

	// Set content type based on file extension
	contentType := "application/octet-stream"
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

	// Serve file with Range support
	c.Set("Content-Type", contentType)
	c.Set("Accept-Ranges", "bytes")
	c.Set("Content-Length", strconv.FormatInt(track.FileSize, 10))

	// Use SendFile which handles Range requests automatically
	return c.SendFile(track.Path)
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