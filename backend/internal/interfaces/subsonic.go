package interfaces

// SubsonicSong represents a track from any Subsonic-compatible server.
type SubsonicSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
}

// SubsonicClient defines the interface for Subsonic-compatible music servers
// (Gonic, Navidrome, etc.).
type SubsonicClient interface {
	TriggerScan() (bool, error)
	Search3(query string) ([]SubsonicSong, error)
	HealthCheck() error
}
