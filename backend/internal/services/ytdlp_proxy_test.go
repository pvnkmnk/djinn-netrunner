package services

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubYtDlp is an executable stand-in for the yt-dlp binary. It prints its
// full argument vector, space-separated on one line, and exits non-zero, so
// the service reports the vector in its error message and the test can assert
// exactly what the downloader was asked to run. Every argument the service
// builds (flags, whitelist formats, paths under t.TempDir) is space-free, so
// a single echo suffices on both platforms.
func writeStubYtDlp(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "yt-dlp.cmd")
		script := "@echo %*\r\nexit /b 1\r\n"
		require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
		return path
	}
	path := filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\necho \"$*\"\nexit 1\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func stubService(bin string) *YtdlpService {
	// The pre-flight walk must succeed for the exec to happen at all, so the
	// source URL is served by a canned transport through the real guarded
	// client — a public host the guard allows, one hop, status 200.
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/track.mp3": {status: http.StatusOK},
	}}
	s := &YtdlpService{ytdlpPath: bin}
	s.resolveClient = guardedClient(canned)
	return s
}

func argsFor(t *testing.T, s *YtdlpService) []string {
	t.Helper()
	_, err := s.DownloadAudio(context.Background(), "http://93.184.216.34/track.mp3", t.TempDir(), "flac")
	require.Error(t, err, "the stub exits non-zero after printing its argument vector")
	const prefix = "yt-dlp failed: "
	require.True(t, strings.HasPrefix(err.Error(), prefix), "unexpected error shape: %v", err)
	body := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(err.Error(), prefix)), ":")
	args := strings.Fields(body)
	return args
}

// The boundary is only real if the downloader is actually pointed at it: the
// proxy must reach the exec as --proxy <url>. Removing the --proxy append (or
// the wiring from the environment) turns this red.
func TestYtdlpService_ProxyFlagReachesTheDownloader(t *testing.T) {
	s := stubService(writeStubYtDlp(t, t.TempDir()))
	s.proxy = "http://egress-proxy:3128"

	args := argsFor(t, s)
	require.Contains(t, args, "--proxy", "the downloader must be handed --proxy")
	i := indexOf(args, "--proxy")
	require.GreaterOrEqual(t, i, 0)
	require.Less(t, i+1, len(args), "--proxy must be followed by its value")
	require.Equal(t, "http://egress-proxy:3128", args[i+1])
}

// Direct egress is the default outside the overlays: no proxy configured, no
// --proxy flag — the flag must not appear with an empty value, which yt-dlp
// would reject.
func TestYtdlpService_NoProxyMeansNoFlag(t *testing.T) {
	s := stubService(writeStubYtDlp(t, t.TempDir()))

	args := argsFor(t, s)
	require.NotContains(t, args, "--proxy")
}

// The wiring runs at construction, from the environment, like the path and JS
// runtime settings beside it.
func TestNewYtdlpService_ReadsProxyFromEnvironment(t *testing.T) {
	bin := writeStubYtDlp(t, t.TempDir())
	t.Setenv("YTDLP_PATH", bin)
	t.Setenv("YTDLP_PROXY", "http://egress-proxy:3128")

	s := NewYtdlpService()
	require.Equal(t, "http://egress-proxy:3128", s.proxy)

	t.Setenv("YTDLP_PROXY", "")
	s = NewYtdlpService()
	require.Empty(t, s.proxy)
}

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}
