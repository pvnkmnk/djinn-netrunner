package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// DJI-545, AC4 on the surface an operator actually reads. The score column was
// a plain int, so this printed a confident "(score: 0%)" for every acquisition
// in the database - all of them unscored, none of them measured, for the whole
// life of the product. Pointing it at a nullable pointer without changing the
// format string would have printed "%!d(*int=<nil>)", which is worse: honest
// noise in a column that was lying.
func TestFormatAcoustIDScore_DistinguishesUnscoredFromZero(t *testing.T) {
	require.Equal(t, "unscored", formatAcoustIDScore(nil),
		"no lookup ever ran for this acquisition, so it must not render a score")
	require.Equal(t, "0%", formatAcoustIDScore(scorePtr(0)),
		"a genuine zero is a measurement and must still read as one")
	require.Equal(t, "87%", formatAcoustIDScore(scorePtr(87)))
}

func scorePtr(n int) *int { return &n }
