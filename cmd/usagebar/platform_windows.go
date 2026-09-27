//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/term"
)

// resizePollInterval bounds how long a pane resize goes unnoticed on Windows.
// It is short enough to feel like a live re-layout while dragging and long
// enough that the size query costs nothing measurable.
const resizePollInterval = 200 * time.Millisecond

// resizeEvents delivers one value per terminal resize. Windows has no SIGWINCH,
// so a poller compares the console size against the last observed one and
// emits an event on change. The returned stop function ends the poller; the
// channel is never closed.
func resizeEvents() (<-chan struct{}, func()) {
	events := make(chan struct{}, 1)
	done := make(chan struct{})
	fd := int(os.Stdout.Fd())
	go func() {
		ticker := time.NewTicker(resizePollInterval)
		defer ticker.Stop()
		lastW, lastH, _ := term.GetSize(fd)
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			w, h, err := term.GetSize(fd)
			if err != nil || (w == lastW && h == lastH) {
				continue
			}
			lastW, lastH = w, h
			// Non-blocking: one pending event already triggers a re-layout.
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	return events, func() { close(done) }
}

// detachFromParent starts cmd in its own process group without a console so
// the background watcher survives the Herdr hook process that spawned it and
// does not flash a console window.
func detachFromParent(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008 // DETACHED_PROCESS
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
