package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireProbeTools skips a test when the ffmpeg toolchain (which ships
// ffprobe) is not installed, mirroring the ffmpeg skips elsewhere in this
// package. CI installs ffmpeg; a bare checkout may not.
func requireProbeTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
}

// writeSineWav generates a short, genuinely decodable audio file so the
// accepting path is exercised against real bytes rather than a fixture.
func writeSineWav(t *testing.T, path string) {
	t.Helper()

	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg could not generate test audio: %v: %s", err, out)
	}
}

func TestAudioProbe_RejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.mp3")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	_, err := NewAudioProbe().Probe(context.Background(), path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
	assert.False(t, errors.Is(err, ErrProbeUnavailable),
		"an empty file is a bad file, not an absent tool")
}

func TestAudioProbe_ReportsUnavailableWhenBinaryMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.mp3")
	require.NoError(t, os.WriteFile(path, []byte("bytes"), 0o644))

	probe := NewAudioProbeWithFFprobe("definitely-not-a-real-ffprobe-binary")
	_, err := probe.Probe(context.Background(), path)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProbeUnavailable,
		"callers must be able to tell an absent tool from a bad file, and must not reject downloads when it is absent")
}

func TestAudioProbe_RejectsNonAudioFile(t *testing.T) {
	requireProbeTools(t)

	path := filepath.Join(t.TempDir(), "not-audio.mp3")
	require.NoError(t, os.WriteFile(path, []byte("<html>404 Not Found</html>"), 0o644))

	_, err := NewAudioProbe().Probe(context.Background(), path)

	require.Error(t, err, "a renamed text file is not playable audio")
	assert.False(t, errors.Is(err, ErrProbeUnavailable),
		"ffprobe ran and rejected the file, so this is not a missing-tool case")
}

func TestAudioProbe_AcceptsRealAudio(t *testing.T) {
	requireProbeTools(t)

	path := filepath.Join(t.TempDir(), "tone.wav")
	writeSineWav(t, path)

	result, err := NewAudioProbe().Probe(context.Background(), path)

	require.NoError(t, err)
	assert.Equal(t, "wav", result.FormatName)
	assert.NotEmpty(t, result.Codecs, "a real file must report an audio stream")
	assert.Greater(t, result.Duration, time.Duration(0))
	assert.Less(t, result.Duration, 5*time.Second)
	assert.Positive(t, result.SizeBytes)
}

// MetadataExtractor is the composition point the acquisition pipeline holds, so
// exporting the probe through it must keep both tools configurable together.
func TestMetadataExtractor_ProbeAudioUsesConfiguredBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.mp3")
	require.NoError(t, os.WriteFile(path, []byte("bytes"), 0o644))

	extractor := NewMetadataExtractorWithTools("", "definitely-not-a-real-ffprobe-binary")
	_, err := extractor.ProbeAudio(context.Background(), path)

	assert.ErrorIs(t, err, ErrProbeUnavailable)
}
