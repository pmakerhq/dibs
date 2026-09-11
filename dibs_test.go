package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionKeyFor_StableForSameInputs(t *testing.T) {
	a := sessionKeyFor(4821, 1757577600000)
	b := sessionKeyFor(4821, 1757577600000)
	assert.Equal(t, a, b, "same pid+start time must hash to the same session key")
}

func TestSessionKeyFor_DiffersOnPidReuse(t *testing.T) {
	// Same pid, different start time simulates the OS recycling a pid
	// after the original shell exited: must be treated as a new session.
	a := sessionKeyFor(4821, 1757577600000)
	b := sessionKeyFor(4821, 1757581200000)
	assert.NotEqual(t, a, b, "recycled pid with a different start time must be a different session")
}

func TestRangeFor_KnownAndUnknownServices(t *testing.T) {
	assert.Equal(t, [2]int{15400, 15499}, rangeFor("postgresql"))
	assert.Equal(t, [2]int{19200, 19299}, rangeFor("opensearch"))
	assert.Equal(t, genericRange, rangeFor("some-unknown-service"))
}

func TestRangeFor_ConfigOverridesDefault(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "dibs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "dibs", "config.json"), []byte(`{
		"ranges": {
			"postgresql": [16000, 16009],
			"generic": [21000, 21009]
		}
	}`), 0o644))

	assert.Equal(t, [2]int{16000, 16009}, rangeFor("postgresql"), "config override must beat the built-in default")
	assert.Equal(t, [2]int{19200, 19299}, rangeFor("opensearch"), "services not overridden keep their built-in default")
	assert.Equal(t, [2]int{21000, 21009}, rangeFor("some-unknown-service"), "\"generic\" override must beat the built-in generic range")
}

func TestPickPortInRange_SkipsTakenAndFindsFree(t *testing.T) {
	r := [2]int{41000, 41005}
	taken := map[int]bool{41000: true, 41001: true}

	p, err := pickPortInRange(r, taken)
	require.NoError(t, err)
	assert.Equal(t, 41002, p, "must pick the first free, untaken port in range")
}

func TestPickPortInRange_ErrorsWhenExhausted(t *testing.T) {
	r := [2]int{41010, 41011}
	taken := map[int]bool{41010: true, 41011: true}

	_, err := pickPortInRange(r, taken)
	assert.Error(t, err, "must error when every port in range is taken")
}

func TestCmdGet_SameSessionReusesPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p1, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	p2, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	assert.Equal(t, p1, p2, "repeated get in the same process (same session) must return the same port")
}

func TestCmdRelease_FreesPortForReallocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p1, err := cmdGet("dibs-test-service-b")
	require.NoError(t, err)

	require.NoError(t, cmdRelease("dibs-test-service-b"))

	entries, err := cmdList()
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, "dibs-test-service-b", e.Service, "released service must not remain in the registry")
	}

	p2, err := cmdGet("dibs-test-service-b")
	require.NoError(t, err)
	assert.Equal(t, p1, p2, "same session re-requesting after release gets the lowest free port again")
}
