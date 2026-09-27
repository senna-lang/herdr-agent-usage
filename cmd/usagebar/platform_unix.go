//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// resizeEvents delivers one value per terminal resize. Unix terminals report
// resizes through SIGWINCH, so the kernel signal is the event source. The
// returned stop function unsubscribes; the channel is never closed.
func resizeEvents() (<-chan struct{}, func()) {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	events := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-winch:
			}
			// Non-blocking: one pending event already triggers a re-layout.
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	return events, func() {
		signal.Stop(winch)
		close(done)
	}
}

// detachFromParent starts cmd in its own session so the background watcher
// survives the Herdr hook process that spawned it.
func detachFromParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
