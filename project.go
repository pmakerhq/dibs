package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// statTimeout bounds how long a single stat can block a `dibs` call — and,
// during gc, the machine-wide registry lock — when a project's directory
// sits on a stale network mount.
// ponytail: fixed ceiling, no backoff; revisit if a slow-but-alive mount
// ever needs longer than this to answer.
const statTimeout = 2 * time.Second

func statWithTimeout(path string) (os.FileInfo, error) {
	type result struct {
		fi  os.FileInfo
		err error
	}
	ch := make(chan result, 1)
	go func() {
		fi, err := os.Stat(path)
		ch <- result{fi, err}
	}()
	select {
	case r := <-ch:
		return r.fi, r.err
	case <-time.After(statTimeout):
		return nil, fmt.Errorf("stat %s: timed out after %s", path, statTimeout)
	}
}

// dirIdentity returns the device and inode of path, so a registry entry can
// tell "the same directory that has always been here" apart from "a new
// directory recreated at the same path after the old one was deleted".
func dirIdentity(path string) (dev, ino uint64, ok bool) {
	fi, err := statWithTimeout(path)
	if err != nil {
		return 0, 0, false
	}
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), true
}

// projectRoot identifies the calling project by directory: the nearest
// ancestor of the working directory holding a .git entry, or the working
// directory itself when there is none.
func projectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Symlinks are resolved so the same project reached through two paths
	// maps to one allocation; case differences are handled by sameProject,
	// since EvalSymlinks leaves casing untouched.
	dir, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	return projectRootOf(dir), nil
}

func projectRootOf(dir string) string {
	for d := dir; ; {
		_, err := statWithTimeout(filepath.Join(d, ".git"))
		if err == nil {
			return d
		}
		if !errors.Is(err, fs.ErrNotExist) {
			// Can't tell whether .git exists here (permission, network
			// hiccup, or our own timeout) — stop rather than risk climbing
			// past the true root.
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// projectAlive reports whether a recorded project path is still a project
// dibs can hand ports back to. It stops being one when the directory is
// deleted, or when it stopped being its own root — `git init` in a parent
// turns a former project into a subdirectory of a new one, and its entry
// would otherwise stay reserved with no way to release it. Errors other
// than "not found" (an unplugged drive, an unreachable network mount)
// deliberately keep the entry: losing a port because a disk was detached
// would break the guarantee that a project's port is stable.
func projectAlive(path string) bool {
	fi, err := statWithTimeout(path)
	// ENOTDIR means an ancestor that used to be a directory is now a plain
	// file — permanent, not transient, so it's treated like "not found".
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false
	}
	if err != nil {
		return true
	}
	return fi.IsDir() && projectRootOf(path) == path
}

// sameProject reports whether two paths name the same project directory.
// String equality is the common case; os.SameFile covers a repo reached
// through a differently-cased path on a case-insensitive filesystem
// (macOS APFS) or through a bind mount. If a stat fails (permission,
// timeout, flaky mount) we fall back to a case-insensitive string compare
// instead of failing closed — projectAlive tolerates the same errors, and
// disagreeing here would let a transient failure look like a different
// project and trigger a duplicate allocation.
func sameProject(a, b string) bool {
	if a == b {
		return true
	}
	fa, errA := statWithTimeout(a)
	fb, errB := statWithTimeout(b)
	if errA == nil && errB == nil {
		return os.SameFile(fa, fb)
	}
	return strings.EqualFold(a, b)
}
