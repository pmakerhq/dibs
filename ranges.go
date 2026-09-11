package main

import (
	"fmt"
	"net"
	"sort"
)

// defaultRanges are the built-in port ranges for known services.
var defaultRanges = map[string][2]int{
	"postgresql": {15400, 15499},
	"opensearch": {19200, 19299},
}

// genericRange is used for any service without a dedicated range.
var genericRange = [2]int{20000, 29999}

func rangeFor(service string) [2]int {
	overrides := loadRangeOverrides()
	if r, ok := overrides[service]; ok {
		return r
	}
	if r, ok := defaultRanges[service]; ok {
		return r
	}
	if r, ok := overrides["generic"]; ok {
		return r
	}
	return genericRange
}

// portFree does a real bind/close to confirm the port isn't held by some
// process outside dibs' own registry (e.g. left running from a previous,
// unmanaged session).
func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// pickPortInRange returns the first port in [r[0], r[1]] that isn't in
// taken and is actually free on the host.
func pickPortInRange(r [2]int, taken map[int]bool) (int, error) {
	for p := r[0]; p <= r[1]; p++ {
		if taken[p] {
			continue
		}
		if portFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in range %d-%d", r[0], r[1])
}

// effectiveRanges merges the built-in per-service ranges with config
// overrides (overrides win), excluding the "generic" fallback: generic is
// meant to cover any service *without* a dedicated range, so it overlapping
// a named range is by design, not a conflict.
func effectiveRanges() map[string][2]int {
	merged := map[string][2]int{}
	for svc, r := range defaultRanges {
		merged[svc] = r
	}
	for svc, r := range loadRangeOverrides() {
		if svc == "generic" {
			continue
		}
		merged[svc] = r
	}
	return merged
}

// checkRangeOverlaps reports every pair of named-service ranges (built-in or
// config-overridden) whose bounds overlap, since two such services could be
// requested concurrently and collide on the same port.
func checkRangeOverlaps() []string {
	ranges := effectiveRanges()
	names := make([]string, 0, len(ranges))
	for svc := range ranges {
		names = append(names, svc)
	}
	sort.Strings(names)

	var conflicts []string
	for i, a := range names {
		for _, b := range names[i+1:] {
			ra, rb := ranges[a], ranges[b]
			if ra[0] <= rb[1] && rb[0] <= ra[1] {
				conflicts = append(conflicts, fmt.Sprintf("%s [%d-%d] overlaps %s [%d-%d]", a, ra[0], ra[1], b, rb[0], rb[1]))
			}
		}
	}
	return conflicts
}

func pickPort(service string, taken map[int]bool) (int, error) {
	r := rangeFor(service)
	p, err := pickPortInRange(r, taken)
	if err != nil {
		return 0, fmt.Errorf("%w for %s", err, service)
	}
	return p, nil
}
