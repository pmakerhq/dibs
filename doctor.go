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
			fmt.Printf("i %d dead entries will be cleared on next call\n", dead)
		}
	}

	if lp, err := lockPath(); err != nil {
		report(false, "lock file: %v", err)
	} else {
		fl := flock.New(lp)
		locked, err := fl.TryLock()
		if err != nil || !locked {
			report(false, "lock file: held by another process")
		} else {
			report(true, "lock file: free")
			fl.Unlock()
		}
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
