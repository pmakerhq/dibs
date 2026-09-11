package main

import (
	"fmt"
	"net"
)

// defaultRanges are the built-in port ranges for known services.
var defaultRanges = map[string][2]int{
	"postgresql": {15400, 15499},
	"opensearch": {19200, 19299},
}

// genericRange is used for any service without a dedicated range.
var genericRange = [2]int{20000, 29999}

func rangeFor(service string) [2]int {
	if r, ok := defaultRanges[service]; ok {
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

func pickPort(service string, taken map[int]bool) (int, error) {
	r := rangeFor(service)
	p, err := pickPortInRange(r, taken)
	if err != nil {
		return 0, fmt.Errorf("%w for %s", err, service)
	}
	return p, nil
}
