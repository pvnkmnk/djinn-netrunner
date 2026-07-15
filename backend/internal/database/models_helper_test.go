package database

import (
	"testing"
)

func TestQualityProfile_IsMatch(t *testing.T) {
	tests := []struct {
		name           string
		profile        QualityProfile
		format         string
		bitrate        int
		expectedResult bool
	}{
		{
			name: "FLAC match - prefer lossless",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "flac,mp3",
				MinBitrate:     320,
			},
			format:         "flac",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "High quality MP3 match - prefer lossless",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "flac,mp3",
				MinBitrate:     320,
			},
			format:         "mp3",
			bitrate:        320,
			expectedResult: true,
		},
		{
			name: "Low quality MP3 fail - prefer lossless",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "flac,mp3",
				MinBitrate:     320,
			},
			format:         "mp3",
			bitrate:        192,
			expectedResult: false,
		},
		{
			name: "Format not allowed",
			profile: QualityProfile{
				AllowedFormats: "flac",
			},
			format:         "mp3",
			bitrate:        320,
			expectedResult: false,
		},
		{
			name: "Case insensitive and dot handling",
			profile: QualityProfile{
				AllowedFormats: "FLAC",
			},
			format:         ".flac",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "WAV lossless format",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "",
			},
			format:         "wav",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "AIFF lossless format",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "",
			},
			format:         "aiff",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "ALAC lossless format",
			profile: QualityProfile{
				PreferLossless: true,
				AllowedFormats: "",
			},
			format:         "alac",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "No allowed formats - any format matches",
			profile: QualityProfile{
				AllowedFormats: "",
				MinBitrate:     0,
			},
			format:         "mp3",
			bitrate:        128,
			expectedResult: true,
		},
		{
			name: "Prefer lossless false - lossy format above min bitrate",
			profile: QualityProfile{
				PreferLossless: false,
				AllowedFormats: "",
				MinBitrate:     192,
			},
			format:         "mp3",
			bitrate:        256,
			expectedResult: true,
		},
		{
			name: "Prefer lossless false - lossy format below min bitrate",
			profile: QualityProfile{
				PreferLossless: false,
				AllowedFormats: "",
				MinBitrate:     256,
			},
			format:         "mp3",
			bitrate:        192,
			expectedResult: false,
		},
		{
			name: "FLAC with dot prefix",
			profile: QualityProfile{
				AllowedFormats: "",
			},
			format:         ".flac",
			bitrate:        0,
			expectedResult: true,
		},
		{
			name: "Ogg vorbis format",
			profile: QualityProfile{
				AllowedFormats: "flac,ogg",
			},
			format:         "ogg",
			bitrate:        192,
			expectedResult: true,
		},
		{
			name: "Ogg format not in allowed list",
			profile: QualityProfile{
				AllowedFormats: "flac,mp3",
			},
			format:         "ogg",
			bitrate:        192,
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.IsMatch(tt.format, tt.bitrate); got != tt.expectedResult {
				t.Errorf("QualityProfile.IsMatch() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestQualityProfile_GetSearchSuffix(t *testing.T) {
	pref320 := 320
	tests := []struct {
		name     string
		profile  QualityProfile
		expected string
	}{
		{
			name: "Prefer lossless returns flac",
			profile: QualityProfile{
				PreferLossless: true,
			},
			expected: "flac",
		},
		{
			name: "Prefer 320 returns 320",
			profile: QualityProfile{
				PreferLossless: false,
				PreferBitrate:  &pref320,
			},
			expected: "320",
		},
		{
			name: "No preference returns empty",
			profile: QualityProfile{
				PreferLossless: false,
				PreferBitrate:  nil,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.GetSearchSuffix(); got != tt.expected {
				t.Errorf("QualityProfile.GetSearchSuffix() = %v, want %v", got, tt.expected)
			}
		})
	}
}
