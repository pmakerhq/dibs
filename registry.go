package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

type Entry struct {
	Service     string `json:"service"`
	Port        int    `json:"port"`
	Project     string `json:"project"`
	AllocatedAt string `json:"allocated_at"`
}

type Registry struct {
	Allocations []Entry `json:"allocations"`
}

func stateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "state", "dibs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func registryPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.json"), nil
}

func lockPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".lock"), nil
}

// withLock runs fn while holding an exclusive advisory lock on the
// registry's lock file, so concurrent `dibs` calls (e.g. two projects
// starting at once) don't race on registry.json.
func withLock(fn func() error) error {
	lp, err := lockPath()
	if err != nil {
		return err
	}
	fl := flock.New(lp)
	if err := fl.Lock(); err != nil {
		return fmt.Errorf("lock registry: %w", err)
	}
	defer fl.Unlock()

	return fn()
}

func loadRegistry() (*Registry, error) {
	rp, err := registryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(rp)
	if os.IsNotExist(err) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return &Registry{}, nil
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return &reg, nil
}

// saveRegistry writes atomically (temp file + rename) so a crash mid-write
// never leaves a corrupt registry.json behind.
func saveRegistry(reg *Registry) error {
	rp, err := registryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := rp + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, rp)
}

// gc drops entries whose project directory is gone (and, incidentally,
// entries written by the pre-project registry format, which carry no path)
// and returns the surviving entries plus the set of ports they hold.
func gc(reg *Registry) (live []Entry, taken map[int]bool) {
	taken = map[int]bool{}
	for _, e := range reg.Allocations {
		if projectAlive(e.Project) {
			live = append(live, e)
			taken[e.Port] = true
		}
	}
	return live, taken
}

// cmdGet returns the port for (project, service), allocating one if this
// project doesn't already hold it.
func cmdGet(service string) (int, error) {
	project, err := projectRoot()
	if err != nil {
		return 0, err
	}
	return allocateFor(service, project)
}

// allocateFor is cmdGet's core, split out so tests can exercise concurrent
// allocation across several projects from a single working directory.
func allocateFor(service, project string) (int, error) {
	var port int
	err := withLock(func() error {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		live, taken := gc(reg)
		collected := len(live) != len(reg.Allocations)

		for _, e := range live {
			if e.Service == service && sameProject(e.Project, project) {
				port = e.Port
				if !collected {
					return nil
				}
				reg.Allocations = live
				return saveRegistry(reg)
			}
		}

		p, err := pickPort(service, taken)
		if err != nil {
			return err
		}
		live = append(live, Entry{
			Service:     service,
			Port:        p,
			Project:     project,
			AllocatedAt: time.Now().Format(time.RFC3339),
		})
		reg.Allocations = live
		port = p
		return saveRegistry(reg)
	})
	return port, err
}

// releaseWhere drops the current project's allocations matching match.
func releaseWhere(match func(Entry) bool) error {
	project, err := projectRoot()
	if err != nil {
		return err
	}
	return withLock(func() error {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		live, _ := gc(reg)
		kept := live[:0]
		for _, e := range live {
			if sameProject(e.Project, project) && match(e) {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == len(reg.Allocations) {
			return nil
		}
		reg.Allocations = kept
		return saveRegistry(reg)
	})
}

// cmdRelease drops the current project's allocation for service, if any.
func cmdRelease(service string) error {
	return releaseWhere(func(e Entry) bool { return e.Service == service })
}

// cmdReleaseAll drops every allocation the current project holds.
func cmdReleaseAll() error {
	return releaseWhere(func(Entry) bool { return true })
}

// cmdList returns the live allocations after GC.
func cmdList() ([]Entry, error) {
	var live []Entry
	err := withLock(func() error {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		live, _ = gc(reg)
		if len(live) == len(reg.Allocations) {
			return nil
		}
		reg.Allocations = live
		return saveRegistry(reg)
	})
	return live, err
}
