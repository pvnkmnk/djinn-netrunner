package services

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// dockerfilePath walks up until backend/Dockerfile turns up, so the test does
// not hard-code how deep in the tree the package sits.
func dockerfilePath(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "backend", "Dockerfile")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find backend/Dockerfile above the test directory")
	return ""
}

// DJI-545, AC1 and AC2. The image shipped without Chromaprint for the project's
// entire history, so every scan and every import reported success while
// fingerprinting nothing, and every acoustid_score in the database was a zero
// nobody had measured.
//
// The install line alone is not the guard: `apk add` resolving successfully is
// not the same claim as fpcalc existing, and a later edit to that line would
// drop the feature quietly. The build has to *assert* it, which is what the
// second check below pins.
func TestDockerfileShipsAndAssertsTheFingerprintBinary(t *testing.T) {
	raw, err := os.ReadFile(dockerfilePath(t))
	require.NoError(t, err)
	dockerfile := string(raw)

	require.Contains(t, dockerfile, "chromaprint",
		"the runtime layer must install Chromaprint, which is where fpcalc comes from")

	// Both stages install packages and only the runtime stage ships. Scanning
	// for the first `apk add` matches the builder's gcc/musl-dev and passes over
	// a runtime layer that had dropped the package.
	var apkLines []string
	for _, line := range strings.Split(dockerfile, "\n") {
		if strings.Contains(line, "apk add") {
			apkLines = append(apkLines, line)
		}
	}
	require.NotEmpty(t, apkLines, "no apk add line found in the Dockerfile")
	require.True(t, slices.ContainsFunc(apkLines, func(l string) bool {
		return strings.Contains(l, "chromaprint")
	}), "chromaprint has to be on an apk add line in the runtime stage, not "+
		"merely mentioned in a comment: %v", apkLines)

	// The assertion has to be a command that *fails the build*, not a comment and
	// not a line that merely mentions the binary. Presence of the text is not the
	// claim: `RUN true command -v fpcalc` contains every word of it and asserts
	// nothing, so the instruction is read whole and has to carry a non-zero exit.
	//
	// Read as one instruction rather than one line, because the check is written
	// with a shell continuation and `exit 1` lands on a later line.
	assertion := dockerfileRunInstructions(dockerfile, "command -v fpcalc")
	require.NotEmpty(t, assertion,
		"the build must run `command -v fpcalc`, so an image whose binary went "+
			"missing cannot be built at all")
	require.Contains(t, assertion, "exit 1",
		"the check has to fail the build, not merely name the binary: %s", assertion)
}

// dockerfileRunInstructions returns the full text of every RUN instruction
// containing needle, continuation lines joined, so a multi-line shell block is
// examined as the one command Docker runs.
func dockerfileRunInstructions(dockerfile, needle string) string {
	var found []string
	lines := strings.Split(strings.ReplaceAll(dockerfile, "\r\n", "\n"), "\n")

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "RUN ") {
			continue
		}

		var block []string
		for ; i < len(lines); i++ {
			current := strings.TrimSpace(lines[i])
			block = append(block, current)
			if !strings.HasSuffix(current, "\\") {
				break
			}
		}

		joined := strings.Join(block, " ")
		if strings.Contains(joined, needle) {
			found = append(found, joined)
		}
	}

	return strings.Join(found, "\n")
}

// The absence half. docs/DEPLOYMENT.md used to carry a troubleshooting entry
// telling operators that the missing binary was expected and not a fault, which
// is only true while the image still lacks it. If that framing comes back, the
// slice has been silently undone in prose.
func TestDeploymentDocNoLongerCallsTheMissingBinaryExpected(t *testing.T) {
	path := filepath.Join(filepath.Dir(filepath.Dir(dockerfilePath(t))), "docs", "DEPLOYMENT.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	doc := string(raw)

	require.NotContains(t, doc, "Install `fpcalc` in `backend/Dockerfile`",
		"the install instructions are the defect: the image is supposed to carry the binary")
	require.NotContains(t, doc, "no Chromaprint binary",
		"the image now ships Chromaprint, so the docs must stop describing its absence")
}
