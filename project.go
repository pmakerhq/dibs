package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

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
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
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
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
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
// (macOS APFS) or through a bind mount.
func sameProject(a, b string) bool {
	if a == b {
		return true
	}
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
