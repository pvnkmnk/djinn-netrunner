package health

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func get(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := map[string]string{}
	require.NoError(t, json.Unmarshal(raw, &body))
	return resp.StatusCode, body
}

func TestHealthz_HealthyWithDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	h, err := New("127.0.0.1:0", db)
	require.NoError(t, err)
	t.Cleanup(h.Stop)

	code, body := get(t, "http://"+h.Addr()+"/healthz")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "ok", body["status"])
}

func TestHealthz_DegradedWhenDBUnreachable(t *testing.T) {
	// A DB whose underlying connection is closed answers SELECT 1 with an
	// error — exactly what the healthcheck must surface as degraded.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	h, err := New("127.0.0.1:0", db)
	require.NoError(t, err)
	t.Cleanup(h.Stop)

	code, body := get(t, "http://"+h.Addr()+"/healthz")
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, "degraded", body["status"])
	require.Equal(t, "unreachable", body["database"])
}

func TestHealthz_NilDBReportsLiveness(t *testing.T) {
	h, err := New("127.0.0.1:0", nil)
	require.NoError(t, err)
	t.Cleanup(h.Stop)

	code, body := get(t, "http://"+h.Addr()+"/healthz")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "ok", body["status"])
}

func TestNew_PortConflictFailsFast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	_, err = New(ln.Addr().String(), nil)
	require.Error(t, err, "binding an already-listening port must fail immediately")
}
