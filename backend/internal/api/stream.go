package api

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// StreamTrack serves an audio file with Range/partial content support.
// It is designed for use with the <audio> HTML element, which sends
// Range headers automatically for seeking and scrub-preview.
func (h *LibraryHandler) StreamTrack(c *fiber.Ctx) error {
	// 1. Auth — user must be set by AuthMiddleware via session cookie
	user, ok := currentUserFromLocals(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "not authenticated",
		})
	}

	// 2. Parse track ID
	trackID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid track id",
		})
	}

	// 3. Load track with ownership check (single query)
	var track struct {
		Path   string
		Format string
	}
	err = h.db.Table("tracks").
		Select("tracks.path, tracks.format").
		Joins("JOIN libraries ON libraries.id = tracks.library_id").
		Where("tracks.id = ? AND libraries.owner_user_id = ?", trackID, user.ID).
		Take(&track).Error
	if err != nil {
		// 404 for both "not found" and "not your track" — no enumeration
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "track not found",
		})
	}

	// 4. Validate and resolve file path (prevent traversal)
	if track.Path == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "file not found",
		})
	}

	absPath, err := filepath.Abs(track.Path)
	if err != nil {
		slog.Warn("stream: path resolution failed", "path", track.Path)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "file not found",
		})
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		slog.Warn("stream: symlink resolution failed", "path", absPath)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "file not found",
		})
	}

	// 5. Open file
	f, err := os.Open(realPath)
	if err != nil {
		slog.Error("stream: file open failed", "path", realPath, "error", err)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "file not found",
		})
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "internal error",
		})
	}

	// 6. Detect content type
	contentType := detectAudioContentType(track.Format, realPath)

	// 7. Serve with Range support
	fileSize := stat.Size()

	// Common headers
	c.Set("Content-Type", contentType)
	c.Set("Accept-Ranges", "bytes")
	c.Set("Cache-Control", "private, no-store")

	rangeHeader := c.Get("Range")

	if rangeHeader == "" {
		// No range — serve entire file (200 OK)
		c.Set("Content-Length", fmt.Sprintf("%d", fileSize))
		return c.SendStream(f, int(fileSize))
	}

	// Parse Range header: "bytes=start-end"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return c.Status(fiber.StatusRequestedRangeNotSatisfiable).Send(nil)
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return c.Status(fiber.StatusRequestedRangeNotSatisfiable).Send(nil)
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return c.Status(fiber.StatusRequestedRangeNotSatisfiable).Send(nil)
	}

	var end int64
	if parts[1] == "" {
		// Open-ended range: "bytes=1000-"
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			c.Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			return c.Status(fiber.StatusRequestedRangeNotSatisfiable).Send(nil)
		}
	}

	// Validate range
	if start < 0 || end >= fileSize || start > end {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		return c.Status(fiber.StatusRequestedRangeNotSatisfiable).Send(nil)
	}

	contentLength := end - start + 1

	// Set 206 headers
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	c.Status(fiber.StatusPartialContent)

	// Serve only the requested byte range
	return c.SendStream(io.NewSectionReader(f, start, contentLength), int(contentLength))
}

// detectAudioContentType returns the MIME type for an audio file based on
// its format field and/or file extension.
func detectAudioContentType(format, path string) string {
	// Prefer the format field from the database (set during scan)
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "ogg", "vorbis":
		return "audio/ogg"
	case "opus":
		return "audio/opus"
	case "wav":
		return "audio/wav"
	case "m4a", "aac", "mp4":
		return "audio/mp4"
	case "ape":
		return "audio/ape"
	case "wma":
		return "audio/x-ms-wma"
	}

	// Fallback: detect from file extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ape":
		return "audio/ape"
	case ".wma":
		return "audio/x-ms-wma"
	}

	return "application/octet-stream"
}
