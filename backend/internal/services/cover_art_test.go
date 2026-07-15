package services

import "testing"

func TestCoverArtCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		artist   string
		album    string
		source   string
		expected string
	}{
		{
			name:     "all lowercase",
			artist:  "pink floyd",
			album:   "the dark side of the moon",
			source:  "musicbrainz",
			expected: "pink floyd:the dark side of the moon:musicbrainz",
		},
		{
			name:     "mixed case artist and album",
			artist:  "Pink Floyd",
			album:   "The Dark Side of the Moon",
			source:  "discogs",
			expected: "pink floyd:the dark side of the moon:discogs",
		},
		{
			name:     "all uppercase",
			artist:  "PINK FLOYD",
			album:   "THE DARK SIDE OF THE MOON",
			source:  "source",
			expected: "pink floyd:the dark side of the moon:source",
		},
		{
			name:     "empty artist",
			artist:  "",
			album:   "the dark side of the moon",
			source:  "musicbrainz",
			expected: ":the dark side of the moon:musicbrainz",
		},
		{
			name:     "empty album",
			artist:  "pink floyd",
			album:   "",
			source:  "discogs",
			expected: "pink floyd::discogs",
		},
		{
			name:     "empty source",
			artist:  "pink floyd",
			album:   "the dark side of the moon",
			source:  "",
			expected: "pink floyd:the dark side of the moon:",
		},
		{
			name:     "all empty strings",
			artist:  "",
			album:   "",
			source:  "",
			expected: "::",
		},
		{
			name:     "unicode artist name",
			artist:  "Björk",
			album:   "Homogenic",
			source:  "musicbrainz",
			expected: "björk:homogenic:musicbrainz",
		},
		{
			name:     "unicode album name",
			artist:  "daft punk",
			album:   "Daft Punk's Greatest Hits",
			source:  "discogs",
			expected: "daft punk:daft punk's greatest hits:discogs",
		},
		{
			name:     "special characters",
			artist:  "Mötley Crüe",
			album:   "Dr. Feelgood",
			source:  "source",
			expected: "mötley crüe:dr. feelgood:source",
		},
		{
			name:     "numerical artist",
			artist:  "10000 Maniacs",
			album:   "In My Tribe",
			source:  "musicbrainz",
			expected: "10000 maniacs:in my tribe:musicbrainz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coverArtCacheKey(tt.artist, tt.album, tt.source)
			if got != tt.expected {
				t.Errorf("coverArtCacheKey(%q, %q, %q) = %q; want %q", tt.artist, tt.album, tt.source, got, tt.expected)
			}
		})
	}
}

func TestParseCoverArtSources(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "single source",
			input:    "musicbrainz",
			expected: []string{"musicbrainz"},
		},
		{
			name:     "multiple sources",
			input:    "source,musicbrainz,discogs",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "with whitespace trimming",
			input:    " source , musicbrainz , discogs ",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "case normalization to lowercase",
			input:    "Source,MusicBrainz,Discogs",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "mixed case and whitespace",
			input:    " SOURCE ,  MusicBrainz  , discogs ",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "empty parts are skipped",
			input:    "source,,musicbrainz,,discogs",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "empty parts with whitespace are skipped",
			input:    "source,  ,musicbrainz,   ,discogs",
			expected: []string{"source", "musicbrainz", "discogs"},
		},
		{
			name:     "all empty parts returns nil",
			input:    ",,,",
			expected: nil,
		},
		{
			name:     "all whitespace only returns nil",
			input:    "   ,   ,   ",
			expected: nil,
		},
		{
			name:     "trailing comma",
			input:    "source,musicbrainz,",
			expected: []string{"source", "musicbrainz"},
		},
		{
			name:     "leading comma",
			input:    ",source,musicbrainz",
			expected: []string{"source", "musicbrainz"},
		},
		{
			name:     "single whitespace source",
			input:    "   ",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCoverArtSources(tt.input)

			if tt.expected == nil {
				if got != nil {
					t.Errorf("parseCoverArtSources(%q) = %v; want nil", tt.input, got)
				}
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("parseCoverArtSources(%q) len = %d; want %d", tt.input, len(got), len(tt.expected))
				return
			}

			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("parseCoverArtSources(%q)[%d] = %q; want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}
