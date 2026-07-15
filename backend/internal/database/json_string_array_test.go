package database

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONStringArray_Value(t *testing.T) {
	tests := []struct {
		name    string
		input   JSONStringArray
		want    driver.Value
		wantErr bool
	}{
		{
			name:    "nil array returns nil",
			input:   nil,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "empty array returns empty JSON array",
			input:   JSONStringArray{},
			want:    []byte("[]"),
			wantErr: false,
		},
		{
			name:    "single element array",
			input:   JSONStringArray{"flac"},
			want:    []byte(`["flac"]`),
			wantErr: false,
		},
		{
			name:    "multiple elements",
			input:   JSONStringArray{"flac", "mp3", "wav"},
			want:    []byte(`["flac","mp3","wav"]`),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Value()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				wantBytes, ok := tt.want.([]byte)
				require.True(t, ok)
				gotBytes, ok := got.([]byte)
				require.True(t, ok)

				// Compare JSON equality (order-independent)
				var wantArr, gotArr []string
				json.Unmarshal(wantBytes, &wantArr)
				json.Unmarshal(gotBytes, &gotArr)
				assert.Equal(t, wantArr, gotArr)
			}
		})
	}
}

func TestJSONStringArray_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    JSONStringArray
		wantErr bool
	}{
		{
			name:    "nil value sets to nil",
			input:   nil,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "[]byte empty array",
			input:   []byte("[]"),
			want:    JSONStringArray{},
			wantErr: false,
		},
		{
			name:    "string empty array",
			input:   "[]",
			want:    JSONStringArray{},
			wantErr: false,
		},
		{
			name:    "[]byte with elements",
			input:   []byte(`["flac","mp3"]`),
			want:    JSONStringArray{"flac", "mp3"},
			wantErr: false,
		},
		{
			name:    "string with elements",
			input:   `["flac","mp3","wav"]`,
			want:    JSONStringArray{"flac", "mp3", "wav"},
			wantErr: false,
		},
		{
			name:    "string single element",
			input:   `["only"]`,
			want:    JSONStringArray{"only"},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   []byte("not valid json"),
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   12345,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got JSONStringArray
			err := got.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJSONStringArray_ValueScan_RoundTrip(t *testing.T) {
	original := JSONStringArray{"flac", "mp3", "wav", "alac"}

	// Value should produce JSON bytes
	val, err := original.Value()
	require.NoError(t, err)

	// Scan should reconstruct the original
	var restored JSONStringArray
	err = restored.Scan(val)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}
