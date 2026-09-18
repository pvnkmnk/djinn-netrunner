package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cannedTransport replays a fixed redirect chain without touching the network.
//
// Hosts in these chains are IP literals on purpose. checkPublicHost resolves a
// literal without DNS, so the guard itself runs for real here (via
// guardedClient) while the test stays offline and deterministic, and a private
// literal is refused for the same reason it would be in production.
type cannedTransport struct {
	hops map[string]cannedHop
	seen []string
}

type cannedHop struct {
	status   int
	location string
}

func (c *cannedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.seen = append(c.seen, req.URL.String())

	hop, ok := c.hops[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}

	header := http.Header{}
	if hop.location != "" {
		header.Set("Location", hop.location)
	}
	return &http.Response{
		StatusCode: hop.status,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

// guardedClient is the walker's client with the network replaced: the chain is
// replayed, every hop passes the same guard the production client carries
// (safeAddressValidator refuses a private hop), and the hop bound is the
// production one.
func guardedClient(canned *cannedTransport) *http.Client {
	// The hop bound comes from the production constructor rather than being
	// restated here, so a bound deleted there fails the chain tests too.
	bound := newRedirectResolvingClient(time.Second).CheckRedirect
	return &http.Client{
		Timeout:       5 * time.Second,
		Transport:     &safeAddressValidator{next: canned},
		CheckRedirect: bound,
	}
}

// A redirect into a private range is the hole this closes: yt-dlp would have
// followed it, because the pre-flight check only saw the first host.
func TestResolveRedirectTarget_RefusesPrivateHop(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/start": {status: http.StatusFound, location: "http://10.0.0.5/secret"},
	}}

	final, err := resolveRedirectTarget(guardedClient(canned), "http://93.184.216.34/start")

	require.Error(t, err, "a hop into a private range must fail the walk")
	assert.ErrorIs(t, err, ErrDisallowedDestination,
		"the verdict must be the guard's, not an ordinary transport error")
	assert.Empty(t, final)
	assert.NotContains(t, canned.seen, "http://10.0.0.5/secret",
		"the refused hop must not be followed")
}

// The legitimate case has to keep working: a chain that stays public resolves to
// its last member, which is what gets handed to the downloader.
func TestResolveRedirectTarget_FollowsPublicChain(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/start": {status: http.StatusMovedPermanently, location: "http://93.184.216.35/real.mp3"},
	}}

	final, err := resolveRedirectTarget(guardedClient(canned), "http://93.184.216.34/start")

	require.NoError(t, err, "a public redirect chain must resolve")
	assert.Equal(t, "http://93.184.216.35/real.mp3", final)
}

// A chain longer than the bound is a failure to walk, not a refusal: nothing was
// found pointing at a private address, so the item should not be abandoned on
// this alone.
func TestResolveRedirectTarget_RefusesOverlongChain(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/loop": {status: http.StatusFound, location: "http://93.184.216.34/loop"},
	}}

	final, err := resolveRedirectTarget(guardedClient(canned), "http://93.184.216.34/loop")

	require.Error(t, err, "an endless chain must not be walked")
	assert.NotErrorIs(t, err, ErrDisallowedDestination,
		"too many redirects is a failure to resolve, not a private-address refusal")
	assert.Contains(t, err.Error(), "hops",
		"the chain must be bounded by the walk's own limit, not the client's default")
	assert.Empty(t, final)
}

// Belt and braces: the name the chain *ended on* is the URL being handed over, so
// it is checked even when the client did not fail a hop itself.
func TestResolveRedirectTarget_ChecksTheHostItEndsOn(t *testing.T) {
	canned := &cannedTransport{hops: map[string]cannedHop{
		"http://93.184.216.34/start": {status: http.StatusFound, location: "http://127.0.0.1/admin"},
	}}
	// No validating transport here on purpose: this asserts the walker's own
	// check of the final host.
	client := &http.Client{Transport: canned, CheckRedirect: hopBoundCheck}

	_, err := resolveRedirectTarget(client, "http://93.184.216.34/start")

	assert.ErrorIs(t, err, ErrDisallowedDestination)
}

// The production client must carry the guard: with anything else the walk would
// observe hops without checking them, which is worse than not walking at all
// because it reads as reassurance.
func TestNewRedirectResolvingClient_CarriesTheGuardAndTheBound(t *testing.T) {
	client := newRedirectResolvingClient(5 * time.Second)

	assert.Same(t, safeTransport, client.Transport,
		"the walk must dial through the guard's transport")
	assert.NotNil(t, client.CheckRedirect, "and bound the number of hops it follows")
}
