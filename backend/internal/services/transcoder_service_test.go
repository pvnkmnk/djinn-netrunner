package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTranscoderService_Transcode tests the Transcode method
func TestTranscoderService_Transcode(t *testing.T) {
	s := NewTranscoderService()

	// Test FFmpeg availability - skip if not available
	if !s.IsFFmpegAvailable() {
		t.Skip("FFmpeg not available, skipping transcoder tests")
	}

	// Create a temporary WAV file for testing
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "test.wav")

	// Create a simple WAV file header (44 bytes) for testing
	wavHeader := []byte{
		'R', 'I', 'F', 'F', 0x24, 0x00, 0x00, 0x00, // ChunkID + ChunkSize
		'W', 'A', 'V', 'E', // Format
		'f', 'm', 't', ' ', 0x10, 0x00, 0x00, 0x00, // Subchunk1ID + Subchunk1Size
		0x01, 0x00, // AudioFormat (PCM)
		0x02, 0x00, // NumChannels (stereo)
		0x44, 0xAC, 0x00, 0x00, // SampleRate (44100)
		0x10, 0xB1, 0x02, 0x00, // ByteRate
		0x04, 0x00, // BlockAlign
		0x10, 0x00, // BitsPerSample
		'd', 'a', 't', 'a', 0x00, 0x00, 0x00, 0x00, // Subchunk2ID + Subchunk2Size
	}

	if err := os.WriteFile(inputPath, wavHeader, 0644); err != nil {
		t.Fatalf("Failed to create test WAV file: %v", err)
	}

	// Test transcoding to MP3
	outputPath, err := s.Transcode(inputPath, "mp3")
	if err != nil {
		t.Fatalf("Transcode failed: %v", err)
	}
	defer os.Remove(outputPath) // Clean up

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("Output file was not created: %v", err)
	}
}

// TestTranscoderService_Transcode_InvalidInput tests error handling
func TestTranscoderService_Transcode_InvalidInput(t *testing.T) {
	s := NewTranscoderService()

	// Test empty input path
	_, err := s.Transcode("", "mp3")
	if err == nil {
		t.Error("Expected error for empty input path")
	}

	// Test empty output format
	_, err = s.Transcode("/tmp/test.wav", "")
	if err == nil {
		t.Error("Expected error for empty output format")
	}

	// Test non-existent input file
	_, err = s.Transcode("/non/existent/file.wav", "mp3")
	if err == nil {
		t.Error("Expected error for non-existent input file")
	}
}

// TestTranscoderService_IsFFmpegAvailable tests the availability check
func TestTranscoderService_IsFFmpegAvailable(t *testing.T) {
	s := NewTranscoderService()
	// This test just ensures the method doesn't panic
	// The actual availability depends on the system
	_ = s.IsFFmpegAvailable()
}
