package interfaces

import (
	"context"
	"github.com/zmb3/spotify/v2"
)

// SpotifyClientProvider abstracts the retrieval of an authenticated Spotify client
type SpotifyClientProvider interface {
	GetClient(ctx context.Context, userID uint64) (*spotify.Client, error)
}
