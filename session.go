package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// isAlive reports whether pid is still running the same process that
// started at startedAt (guards against PID reuse after the shell exited).
func isAlive(pid int32, startedAt int64) bool {
	cur, err := processStartTime(pid)
	if err != nil {
		return false
	}
	return cur == startedAt
}

// sessionKeyFor derives the session key from a shell's pid and start time.
func sessionKeyFor(pid int32, startedAt int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", pid, startedAt)))
	return hex.EncodeToString(h[:])[:12]
}

// currentSession identifies the calling shell by its PID and start time,
// so repeated calls from the same terminal resolve to the same key, and a
// new terminal (new PID+start time) resolves to a different one.
func currentSession() (key string, pid int32, startedAt int64, err error) {
	pid = int32(os.Getppid())
	startedAt, err = processStartTime(pid)
	if err != nil {
		return "", 0, 0, fmt.Errorf("resolve parent shell (pid %d): %w", pid, err)
	}
	return sessionKeyFor(pid, startedAt), pid, startedAt, nil
}
