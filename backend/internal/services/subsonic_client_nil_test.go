package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A typed-nil *SubsonicClient held in a SubsonicClientInterface is NOT a nil
// interface, so `library == nil` in the acquisition pipeline does not catch it.
// On a stack with no media server configured the worker left the client nil,
// every acquisition reached stageCheckLibraryIndex, called Search3 on the nil
// receiver, and panicked inside doRequest — failing the whole job with
// "50/50 items pending retry" on every attempt.
func TestSubsonicClient_NilReceiverReturnsErrorInsteadOfPanicking(t *testing.T) {
	var c *SubsonicClient

	songs, err := c.Search3("anything")
	require.Error(t, err, "a nil client must report an error, not dereference itself")
	require.Nil(t, songs)

	ok, err := c.TriggerScan()
	require.Error(t, err)
	require.False(t, ok)

	song, err := c.GetSong("some-id")
	require.Error(t, err)
	require.Nil(t, song)

	_, err = c.GetScanStatus()
	require.Error(t, err)

	require.False(t, c.HealthCheck())
}

// The wiring contract the worker depends on: with no media server configured
// the handler receives a genuine nil interface, so the stage short-circuits to
// "not in the library" and acquisition continues to the Soulseek search. (With
// a typed nil it would instead call into the client — which now returns an
// error rather than panicking, but still does needless work.)
func TestStageCheckLibraryIndex_NoLibraryProceeds(t *testing.T) {
	h := NewAcquisitionHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.Nil(t, h.library)

	skip, err := h.stageCheckLibraryIndex(&acquisitionPipeline{})
	require.NoError(t, err)
	require.False(t, skip, "a missing library server must not mark items as already indexed")
}
