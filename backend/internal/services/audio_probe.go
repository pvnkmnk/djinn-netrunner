package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// audioProbeTimeout bounds one ffprobe run so a pathological file or a hung
// child process can never wedge the worker. Probing reads container headers,
// so even a long lossless file is fast; this is pure headroom.
const audioProbeTimeout = 30 * time.Second

// ErrProbeUnavailable reports that ffprobe itself could not be run — the binary
// is missing, not that the file is bad. Callers must treat this as "cannot
// validate" and carry on: a deployment without ffprobe must not reject every
// download it makes.
var ErrProbeUnavailable = errors.New("ffprobe is not available")

// ProbeResult is what ffprobe reported about a downloaded file.
type ProbeResult struct {
	FormatName string
	Codecs     []string // audio codec names, in stream order
	Duration   time.Duration
	SizeBytes  int64
}

// AudioProbe validates that a downloaded file is genuinely playable audio.
// Peers serve truncated rips, HTML error pages renamed to .mp3, and
// zero-length placeholders; the pre-download gate in the acquisition pipeline
// only sees the metadata a peer advertises, so this is the check that runs
// against the actual bytes. ffprobe ships with the ffmpeg toolchain the tag
// writer already depends on, so it adds no new runtime dependency.
type AudioProbe struct {
	// FFprobePath is the ffprobe binary; defaults to PATH lookup ("ffprobe").
	FFprobePath string
}

// NewAudioProbe returns a probe using the ffprobe binary from PATH.
func NewAudioProbe() *AudioProbe {
	return &AudioProbe{FFprobePath: "ffprobe"}
}

// NewAudioProbeWithFFprobe returns a probe using the given binary (mirrors
// NewFFmpegTaggerWithFFmpeg; used by tests).
func NewAudioProbeWithFFprobe(ffprobePath string) *AudioProbe {
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	return &AudioProbe{FFprobePath: ffprobePath}
}

// probeOutput mirrors the subset of `ffprobe -of json` we consume.
type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
	} `json:"format"`
}

// Probe runs ffprobe over path and reports what it found. A file ffprobe cannot
// parse, that carries no audio stream, or whose streams have no usable length
// is not playable audio. Returns ErrProbeUnavailable when ffprobe is missing.
func (p *AudioProbe) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read downloaded file: %w", err)
	}
	if info.Size() == 0 {
		return nil, errors.New("downloaded file is empty")
	}

	probeCtx, cancel := context.WithTimeout(ctx, audioProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx,
		p.FFprobePath,
		"-v", "error",
		"-show_entries", "format=format_name,duration:stream=codec_type,codec_name",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrProbeUnavailable, p.FFprobePath)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := firstStderrLine(string(exitErr.Stderr))
			if msg == "" {
				msg = "ffprobe could not read the file"
			}
			return nil, fmt.Errorf("not playable audio: %s", msg)
		}
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var parsed probeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("could not parse ffprobe output: %w", err)
	}

	result := &ProbeResult{FormatName: parsed.Format.FormatName, SizeBytes: info.Size()}
	for _, stream := range parsed.Streams {
		if stream.CodecType == "audio" {
			result.Codecs = append(result.Codecs, stream.CodecName)
		}
	}
	if len(result.Codecs) == 0 {
		return nil, fmt.Errorf("file contains no audio stream (format %q)", result.FormatName)
	}

	if seconds, parseErr := strconv.ParseFloat(parsed.Format.Duration, 64); parseErr == nil && seconds > 0 {
		result.Duration = time.Duration(seconds * float64(time.Second))
	}
	if result.Duration <= 0 {
		return nil, fmt.Errorf("file reports no usable duration (format %q)", result.FormatName)
	}

	return result, nil
}

// firstStderrLine keeps ffprobe's diagnostics readable inside one job-log line.
func firstStderrLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
