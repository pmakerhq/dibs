package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points HOME and XDG_CONFIG_HOME at empty temp dirs, so a test
// never reads (or writes) the developer's real registry or config.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// writeConfig writes a dibs config.json into an isolated XDG_CONFIG_HOME.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "dibs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}

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
	isolate(t)
	assert.Equal(t, [2]int{15400, 15499}, rangeFor("postgresql"))
	assert.Equal(t, [2]int{19200, 19299}, rangeFor("opensearch"))
	assert.Equal(t, genericRange, rangeFor("some-unknown-service"))
}

func TestRangeFor_ConfigOverridesDefault(t *testing.T) {
	isolate(t)
	writeConfig(t, `{
		"ranges": {
			"postgresql": [16000, 16009],
			"generic": [21000, 21009]
		}
	}`)

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
	isolate(t)

	p1, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	p2, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	assert.Equal(t, p1, p2, "repeated get in the same process (same session) must return the same port")
}

func TestCmdRelease_FreesPortForReallocation(t *testing.T) {
	isolate(t)

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

func TestCmdReleaseAll_FreesEverySessionPort(t *testing.T) {
	isolate(t)

	_, err := cmdGet("dibs-test-service-c")
	require.NoError(t, err)
	_, err = cmdGet("dibs-test-service-d")
	require.NoError(t, err)

	require.NoError(t, cmdReleaseAll())

	entries, err := cmdList()
	require.NoError(t, err)
	assert.Empty(t, entries, "release --all must drop every allocation for the current session")
}

func TestCheckRangeOverlaps_DetectsAndClearsConflicts(t *testing.T) {
	isolate(t)
	assert.Empty(t, checkRangeOverlaps(), "built-in ranges must not overlap")

	writeConfig(t, `{
		"ranges": {
			"redis": [15450, 15460]
		}
	}`)

	conflicts := checkRangeOverlaps()
	require.Len(t, conflicts, 1, "redis range must be flagged as overlapping postgresql's")
	assert.Contains(t, conflicts[0], "postgresql")
	assert.Contains(t, conflicts[0], "redis")
}

func TestAllocateFor_ConcurrentSessionsNeverCollide(t *testing.T) {
	isolate(t)

	// Every synthetic session shares this test process's own pid+start
	// time, so gc() sees them all as genuinely alive; only the session key
	// differs, which is what should keep their ports distinct.
	pid := int32(os.Getpid())
	startedAt, err := processStartTime(pid)
	require.NoError(t, err)

	const sessions = 20
	ports := make([]int, sessions)
	errs := make([]error, sessions)
	var wg sync.WaitGroup
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ports[i], errs[i] = allocateFor("dibs-test-stress", fmt.Sprintf("session-%d", i), pid, startedAt)
		}(i)
	}
	wg.Wait()

	seen := map[int]bool{}
	for i := 0; i < sessions; i++ {
		require.NoError(t, errs[i])
		assert.False(t, seen[ports[i]], "port %d was allocated to more than one session", ports[i])
		seen[ports[i]] = true
	}
}

func TestLoadRangeOverrides_ReportsMalformedConfig(t *testing.T) {
	isolate(t)
	writeConfig(t, `{"ranges": {`)

	_, err := loadRangeOverrides()
	assert.Error(t, err, "a malformed config must be reported, not silently ignored")
	assert.Equal(t, [2]int{15400, 15499}, rangeFor("postgresql"), "allocation still falls back to the built-in range")
}

func TestRangeFor_IgnoresInvalidOverrides(t *testing.T) {
	isolate(t)
	writeConfig(t, `{
		"ranges": {
			"postgresql": [16100, 16000],
			"generic": [0, 100]
		}
	}`)

	assert.Equal(t, [2]int{15400, 15499}, rangeFor("postgresql"), "inverted bounds must fall back to the built-in range")
	assert.Equal(t, genericRange, rangeFor("some-unknown-service"), "a range including port 0 must fall back to the built-in generic range")

	problems := checkRangeBounds()
	require.Len(t, problems, 2, "doctor must surface both invalid ranges")
	assert.Contains(t, problems[0], "generic")
	assert.Contains(t, problems[1], "postgresql")
}

func TestEnvVarName_ShellSafe(t *testing.T) {
	assert.Equal(t, "POSTGRESQL_PORT", envVarName("postgresql"))
	assert.Equal(t, "MY_SERVICE_PORT", envVarName("my-service"))
	assert.Equal(t, "MY_SERVICE_PORT", envVarName("my.service"))
	assert.Equal(t, "_3SCALE_PORT", envVarName("3scale"), "a leading digit would make the name unassignable in a shell")
}

func TestRunList_JSONReportsLiveAllocations(t *testing.T) {
	isolate(t)
	port, err := cmdGet("dibs-test-json")
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, runList(&buf, true))

	var entries []Entry
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entries), "--json must emit parseable JSON")
	require.Len(t, entries, 1)
	assert.Equal(t, "dibs-test-json", entries[0].Service)
	assert.Equal(t, port, entries[0].Port)
}

func TestRunList_JSONEmitsArrayWhenEmpty(t *testing.T) {
	isolate(t)
	var buf bytes.Buffer
	require.NoError(t, runList(&buf, true))
	assert.Equal(t, "[]", strings.TrimSpace(buf.String()), "scripts consuming --json must always get an array, never null")
}

func TestRunEnv_ExportsOneLinePerService(t *testing.T) {
	isolate(t)
	var buf bytes.Buffer
	require.NoError(t, runEnv(&buf, []string{"postgresql", "my.service"}))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, `^export POSTGRESQL_PORT=\d+$`, lines[0])
	assert.Regexp(t, `^export MY_SERVICE_PORT=\d+$`, lines[1])

	port, err := cmdGet("postgresql")
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("export POSTGRESQL_PORT=%d", port), lines[0], "env must hand out the same port as get")
}

func TestCmdDoctor_PassesCleanStateAndFlagsBadConfig(t *testing.T) {
	isolate(t)
	var buf bytes.Buffer
	require.NoError(t, cmdDoctor(&buf), "a clean state must pass every check")
	assert.NotContains(t, buf.String(), "\u2717")

	writeConfig(t, `{"ranges": {"redis": [15450, 15460]}}`)
	buf.Reset()
	assert.Error(t, cmdDoctor(&buf), "an overlapping range must fail the health gate")
	assert.Contains(t, buf.String(), "range conflict")

	writeConfig(t, `{"ranges": {`)
	buf.Reset()
	assert.Error(t, cmdDoctor(&buf), "a malformed config must fail the health gate")
	assert.Contains(t, buf.String(), "config:")
}
