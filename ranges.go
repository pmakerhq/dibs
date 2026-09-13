package main

import (
	"fmt"
	"net"
	"sort"
)

// genericRange is used for any service without a dedicated range.
var genericRange = [2]int{20000, 29999}

// validRange rejects bounds that can't describe a usable port span, so a
// typo'd config (inverted bounds, port 0, above 65535) falls back to the
// generic range instead of handing out a nonsense port. `dibs doctor`
// reports the ones that were ignored.
func validRange(r [2]int) bool {
	return r[0] >= 1 && r[1] <= 65535 && r[0] <= r[1]
}

func rangeFor(service string) [2]int {
	overrides, _ := loadRangeOverrides()
	if r, ok := overrides[service]; ok && validRange(r) {
		return r
	}
	if r, ok := overrides["generic"]; ok && validRange(r) {
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
	return 0, fmt.Errorf("no free port in range %d-%d (run `dibs list` and release projects you no longer use)", r[0], r[1])
}

// effectiveRanges returns the configured named ranges, excluding the
// "generic" fallback: generic is meant to cover any service *without* a
// dedicated range, so it overlapping a named range is by design, not a
// conflict.
func effectiveRanges() map[string][2]int {
	merged := map[string][2]int{}
	overrides, _ := loadRangeOverrides()
	for svc, r := range overrides {
		if svc == "generic" {
			continue
		}
		merged[svc] = r
	}
	return merged
}

func sortedNames(m map[string][2]int) []string {
	names := make([]string, 0, len(m))
	for svc := range m {
		names = append(names, svc)
	}
	sort.Strings(names)
	return names
}

// checkRangeBounds reports every configured range whose bounds can't describe
// a usable port span; such ranges are ignored at allocation time.
func checkRangeBounds() []string {
	overrides, _ := loadRangeOverrides()

	var problems []string
	for _, svc := range sortedNames(overrides) {
		if r := overrides[svc]; !validRange(r) {
			problems = append(problems, fmt.Sprintf("%s [%d-%d] is not a usable port range, ignored", svc, r[0], r[1]))
		}
	}
	return problems
}

// checkRangeOverlaps reports every pair of configured named-service ranges
// whose bounds overlap, since two such services could be requested
// concurrently and collide on the same port.
func checkRangeOverlaps() []string {
	ranges := effectiveRanges()
	names := sortedNames(ranges)

	var conflicts []string
	for i, a := range names {
		for _, b := range names[i+1:] {
			ra, rb := ranges[a], ranges[b]
			if !validRange(ra) || !validRange(rb) {
				continue
			}
			if ra[0] <= rb[1] && rb[0] <= ra[1] {
				conflicts = append(conflicts, fmt.Sprintf("%s [%d-%d] overlaps %s [%d-%d]", a, ra[0], ra[1], b, rb[0], rb[1]))
			}
		}
	}
	return conflicts
}

// checkRangeUsage reports ranges that are filling up. Project allocations
// never expire on their own, so a range creeping towards full is a problem
// worth surfacing before the next new project gets no port at all. Usage is
// counted by which ports actually fall inside a range right now, not by
// each entry's service: that way narrowing a range after allocations were
// made under a wider one doesn't produce a nonsensical ratio, and two named
// ranges that overlap correctly see each other's allocations eating into
// their shared capacity.
func checkRangeUsage(live []Entry) []string {
	overrides, _ := loadRangeOverrides()
	ranges := map[string][2]int{}
	for svc, r := range overrides {
		if svc != "generic" {
			ranges[svc] = r
		}
	}
	if g, ok := overrides["generic"]; ok && validRange(g) {
		ranges["generic"] = g
	} else {
		ranges["generic"] = genericRange
	}

	var warnings []string
	for _, name := range sortedNames(ranges) {
		r := ranges[name]
		if !validRange(r) {
			continue
		}
		n := 0
		for _, e := range live {
			if e.Port >= r[0] && e.Port <= r[1] {
				n++
			}
		}
		span := r[1] - r[0] + 1
		if n*5 >= span*4 {
			warnings = append(warnings, fmt.Sprintf("range %d-%d (%s) is %d/%d allocated; release projects you no longer use", r[0], r[1], name, n, span))
		}
	}
	sort.Strings(warnings)
	return warnings
}

func pickPort(service string, taken map[int]bool) (int, error) {
	r := rangeFor(service)
	p, err := pickPortInRange(r, taken)
	if err != nil {
		return 0, fmt.Errorf("%w for %s", err, service)
	}
	return p, nil
}
