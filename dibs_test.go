package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points HOME and XDG_CONFIG_HOME at empty temp dirs and moves the
// test into a scratch project directory, so a test never reads (or writes)
// the developer's real registry, config, or project allocations.
func isolate(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return chdirProject(t, t.TempDir())
}

// chdirProject enters dir as the current project and returns the path dibs
// resolves it to (temp dirs are symlinked on macOS).
func chdirProject(t *testing.T, dir string) string {
	t.Helper()
	t.Chdir(dir)
	root, err := projectRoot()
	require.NoError(t, err)
	return root
}

// writeConfig writes a dibs config.json into an isolated XDG_CONFIG_HOME.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "dibs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}

func TestProjectRoot_WalksUpToGitRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	sub := filepath.Join(root, "services", "api")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	fromRoot := chdirProject(t, root)
	fromSub := chdirProject(t, sub)
	assert.Equal(t, fromRoot, fromSub, "a subdirectory of a repo must resolve to the repo root")
}

func TestProjectRoot_FallsBackToWorkingDir(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, resolved, chdirProject(t, dir), "outside a repo, the working directory is the project")
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

func TestCmdGet_SameProjectReusesPort(t *testing.T) {
	isolate(t)

	p1, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	p2, err := cmdGet("dibs-test-service-a")
	require.NoError(t, err)

	assert.Equal(t, p1, p2, "repeated get from the same project must return the same port")
}

func TestCmdGet_PortSurvivesAcrossSubdirectories(t *testing.T) {
	isolate(t)
	root, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	sub := filepath.Join(root, "backend")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	p1, err := cmdGet("dibs-test-subdir")
	require.NoError(t, err)

	chdirProject(t, sub)
	p2, err := cmdGet("dibs-test-subdir")
	require.NoError(t, err)

	assert.Equal(t, p1, p2, "the same repo must get the same port from any of its subdirectories")
}

func TestCmdGet_DifferentProjectsGetDifferentPorts(t *testing.T) {
	isolate(t)
	p1, err := cmdGet("dibs-test-two-projects")
	require.NoError(t, err)

	chdirProject(t, t.TempDir())
	p2, err := cmdGet("dibs-test-two-projects")
	require.NoError(t, err)

	assert.NotEqual(t, p1, p2, "two projects asking for the same service must not collide")
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
	assert.Equal(t, p1, p2, "same project re-requesting after release gets the lowest free port again")
}

func TestCmdReleaseAll_FreesEveryProjectPort(t *testing.T) {
	isolate(t)

	_, err := cmdGet("dibs-test-service-c")
	require.NoError(t, err)
	_, err = cmdGet("dibs-test-service-d")
	require.NoError(t, err)

	require.NoError(t, cmdReleaseAll())

	entries, err := cmdList()
	require.NoError(t, err)
	assert.Empty(t, entries, "release --all must drop every allocation for the current project")
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

func TestAllocateFor_ConcurrentProjectsNeverCollide(t *testing.T) {
	isolate(t)

	base := t.TempDir()
	const projects = 20
	paths := make([]string, projects)
	for i := range paths {
		paths[i] = filepath.Join(base, fmt.Sprintf("project-%d", i))
		require.NoError(t, os.MkdirAll(paths[i], 0o755))
	}

	ports := make([]int, projects)
	errs := make([]error, projects)
	var wg sync.WaitGroup
	for i := 0; i < projects; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ports[i], errs[i] = allocateFor("dibs-test-stress", paths[i])
		}(i)
	}
	wg.Wait()

	seen := map[int]bool{}
	for i := 0; i < projects; i++ {
		require.NoError(t, errs[i])
		assert.False(t, seen[ports[i]], "port %d was allocated to more than one project", ports[i])
		seen[ports[i]] = true
	}
}

func TestGC_DropsDeletedProjectsAndKeepsLiveOnes(t *testing.T) {
	isolate(t)

	base := t.TempDir()
	gone, stays := filepath.Join(base, "deleted"), filepath.Join(base, "kept")
	require.NoError(t, os.MkdirAll(gone, 0o755))
	require.NoError(t, os.MkdirAll(stays, 0o755))

	gonePort, err := allocateFor("dibs-test-gc", gone)
	require.NoError(t, err)
	keptPort, err := allocateFor("dibs-test-gc", stays)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(gone))

	entries, err := cmdList()
	require.NoError(t, err)
	ports := map[int]bool{}
	for _, e := range entries {
		ports[e.Port] = true
	}
	assert.False(t, ports[gonePort], "a deleted project's port must be reclaimed")
	assert.True(t, ports[keptPort], "gc must not touch projects that still exist")
}

func TestGC_KeepsProjectsOnUnreadableParent(t *testing.T) {
	isolate(t)

	base := t.TempDir()
	project := filepath.Join(base, "vault", "repo")
	require.NoError(t, os.MkdirAll(project, 0o755))
	port, err := allocateFor("dibs-test-unreadable", project)
	require.NoError(t, err)

	// Simulates a detached drive or unreachable mount: stat fails with a
	// permission error, not "not found", so the reservation must survive.
	require.NoError(t, os.Chmod(filepath.Join(base, "vault"), 0o000))
	t.Cleanup(func() { os.Chmod(filepath.Join(base, "vault"), 0o755) })
	if _, err := os.Stat(project); err == nil {
		t.Skip("running with rights that ignore directory permissions; cannot simulate an unreachable project")
	} else {
		require.False(t, errors.Is(err, fs.ErrNotExist), "the simulated failure must not be a plain \"not found\"")
	}

	entries, err := cmdList()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, port, entries[0].Port, "an unreachable project must keep its port")
}

func TestGC_ReclaimsProjectSwallowedByANewRepo(t *testing.T) {
	isolate(t)

	parent := t.TempDir()
	child := filepath.Join(parent, "backend")
	require.NoError(t, os.MkdirAll(child, 0o755))

	chdirProject(t, child)
	orphaned, err := cmdGet("dibs-test-swallowed")
	require.NoError(t, err)

	// git init at the parent makes `child` a subdirectory of a new project,
	// so its old entry can never be addressed again and must be reclaimed.
	require.NoError(t, os.MkdirAll(filepath.Join(parent, ".git"), 0o755))

	entries, err := cmdList()
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, orphaned, e.Port, "an unaddressable entry must not keep its port reserved")
	}
}

func TestCmdGet_MatchesProjectAcrossPathCasing(t *testing.T) {
	isolate(t)

	dir := filepath.Join(t.TempDir(), "Shop")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	lower := filepath.Join(filepath.Dir(dir), "shop")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("filesystem is case-sensitive; the two paths are genuinely different projects")
	}

	p1, err := allocateFor("dibs-test-casing", dir)
	require.NoError(t, err)
	p2, err := allocateFor("dibs-test-casing", lower)
	require.NoError(t, err)
	assert.Equal(t, p1, p2, "one directory reached through two casings is one project")
}

func TestCheckRangeUsage_WarnsWhenRangeNearlyFull(t *testing.T) {
	isolate(t)
	writeConfig(t, `{"ranges": {"dibs-test-tiny": [45000, 45004]}}`)

	var live []Entry
	for i := 0; i < 4; i++ {
		live = append(live, Entry{Service: "dibs-test-tiny", Port: 45000 + i})
	}
	warnings := checkRangeUsage(live)
	require.Len(t, warnings, 1, "a range at 4/5 must be flagged")
	assert.Contains(t, warnings[0], "4/5")

	assert.Empty(t, checkRangeUsage(live[:2]), "a range at 2/5 must stay quiet")
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
	assert.NotEmpty(t, entries[0].Project, "each entry must carry the project it belongs to")
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

func TestAllocateFor_PathRecreationGetsFreshIdentity(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	project := filepath.Join(base, "app")
	require.NoError(t, os.MkdirAll(project, 0o755))

	_, err := allocateFor("dibs-test-recreate", project)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(project))
	require.NoError(t, os.MkdirAll(project, 0o755))
	wantDev, wantIno, ok := dirIdentity(project)
	require.True(t, ok)

	_, err = allocateFor("dibs-test-recreate", project)
	require.NoError(t, err)

	entries, err := cmdList()
	require.NoError(t, err)
	require.Len(t, entries, 1, "the stale entry must be replaced, not kept alongside a new one")
	assert.Equal(t, wantDev, entries[0].Dev)
	assert.Equal(t, wantIno, entries[0].Ino, "the entry must reflect the recreated directory, not the deleted one")
}

func TestProjectAlive_ENOTDIRIsTreatedAsGone(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	project := filepath.Join(parent, "repo")
	require.NoError(t, os.MkdirAll(project, 0o755))

	port, err := allocateFor("dibs-test-enotdir", project)
	require.NoError(t, err)

	// Replace the ancestor directory with a plain file: os.Stat(project) now
	// fails with ENOTDIR, a permanent condition, not "not found".
	require.NoError(t, os.RemoveAll(parent))
	require.NoError(t, os.WriteFile(parent, []byte("not a directory"), 0o644))
	t.Cleanup(func() { os.RemoveAll(parent) })

	_, statErr := os.Stat(project)
	require.True(t, errors.Is(statErr, syscall.ENOTDIR), "the simulated failure must be ENOTDIR")

	entries, err := cmdList()
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, port, e.Port, "an ancestor permanently replaced by a file must not keep the entry alive forever")
	}
}

func TestCheckRangeUsage_WarnsOnSharedOverlapExhaustion(t *testing.T) {
	isolate(t)
	writeConfig(t, `{"ranges": {"dibs-test-a": [45000, 45004], "dibs-test-b": [45001, 45004]}}`)

	var live []Entry
	for p := 45001; p <= 45004; p++ {
		live = append(live, Entry{Service: "dibs-test-b", Port: p})
	}

	warnings := checkRangeUsage(live)
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, "dibs-test-a", "a's shared sub-range being exhausted by b's allocations must still warn, even though a has no allocations of its own")
}
