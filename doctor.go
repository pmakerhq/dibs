package main

import (
	"fmt"

	"github.com/gofrs/flock"
)

// cmdDoctor runs a handful of sanity checks on dibs' on-disk state and
// config, printing one line per check. It returns an error if any check
// fails, so `dibs doctor` can be used as a CI/shell health gate.
func cmdDoctor() error {
	healthy := true
	report := func(ok bool, format string, args ...any) {
		mark := "✓"
		if !ok {
			mark = "✗"
			healthy = false
		}
		fmt.Printf("%s %s\n", mark, fmt.Sprintf(format, args...))
	}
	info := func(format string, args ...any) {
		fmt.Printf("i %s\n", fmt.Sprintf(format, args...))
	}

	if dir, err := stateDir(); err != nil {
		report(false, "state dir: %v", err)
	} else {
		report(true, "state dir: %s", dir)
	}

	reg, err := loadRegistry()
	if err != nil {
		report(false, "registry.json: %v", err)
	} else {
		report(true, "registry.json: %d entries", len(reg.Allocations))
		live, _ := gc(reg)
		if dead := len(reg.Allocations) - len(live); dead > 0 {
			info("%d dead entries will be cleared on next call", dead)
		}
	}

	// A held lock just means another dibs call is in flight, which is
	// normal — informational, never a health failure.
	if lp, err := lockPath(); err != nil {
		report(false, "lock file: %v", err)
	} else {
		fl := flock.New(lp)
		if locked, err := fl.TryLock(); err != nil {
			report(false, "lock file: %v", err)
		} else if !locked {
			info("lock file: currently held by another dibs call")
		} else {
			report(true, "lock file: free")
			fl.Unlock()
		}
	}

	cp, _ := configPath()
	if overrides, err := loadRangeOverrides(); err != nil {
		report(false, "config: %v", err)
	} else {
		report(true, "config: %s (%d range overrides)", cp, len(overrides))
	}

	for _, p := range checkRangeBounds() {
		report(false, "invalid range: %s", p)
	}

	conflicts := checkRangeOverlaps()
	if len(conflicts) == 0 {
		report(true, "no port range conflicts")
	}
	for _, c := range conflicts {
		report(false, "range conflict: %s", c)
	}

	if !healthy {
		return fmt.Errorf("issues found")
	}
	return nil
}
