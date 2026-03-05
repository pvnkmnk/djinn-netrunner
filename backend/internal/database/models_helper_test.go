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
