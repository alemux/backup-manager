package sync

import (
	"fmt"
	"os/exec"
	"sync"
)

// ProcessTracker allows external code (e.g. the orchestrator) to track and
// stop the currently running sync process (rsync or lftp).
type ProcessTracker struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// NewProcessTracker creates a new ProcessTracker.
func NewProcessTracker() *ProcessTracker {
	return &ProcessTracker{}
}

// Set stores the currently running command.
func (pt *ProcessTracker) Set(cmd *exec.Cmd) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.cmd = cmd
}

// Stop kills the running process, if any.
func (pt *ProcessTracker) Stop() error {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if pt.cmd == nil || pt.cmd.Process == nil {
		return fmt.Errorf("no running process")
	}
	return pt.cmd.Process.Kill()
}

// Clear removes the reference to the running command.
func (pt *ProcessTracker) Clear() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.cmd = nil
}

// IsRunning returns true if a command is currently tracked.
func (pt *ProcessTracker) IsRunning() bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.cmd != nil
}
