package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestTheFirstInterruptCancelsTheRun covers the first interrupt: it
// cancels the run and does not end the process.
func TestTheFirstInterruptCancelsTheRun(t *testing.T) {
	interrupts := make(chan os.Signal, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	exited := make(chan int, 1)
	stopped := make(chan struct{})
	go watchInterrupts(interrupts, cancel, func() error { close(stopped); return nil }, finished, func(code int) { exited <- code })

	interrupts <- syscall.SIGINT
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the first interrupt did not cancel the run")
	}
	select {
	case code := <-exited:
		t.Fatalf("the first interrupt exited with %d", code)
	case <-stopped:
		t.Fatal("the first interrupt stopped the session")
	case <-time.After(100 * time.Millisecond):
	}
	close(finished)
}

// TestTheSecondInterruptStopsAndExits130 covers the second interrupt:
// it stops the session and ends the process with 130.
func TestTheSecondInterruptStopsAndExits130(t *testing.T) {
	interrupts := make(chan os.Signal, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	exited := make(chan int, 1)
	stopped := make(chan struct{})
	go watchInterrupts(interrupts, cancel, func() error { close(stopped); return nil }, finished, func(code int) { exited <- code })

	interrupts <- syscall.SIGINT
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the first interrupt did not cancel the run")
	}
	interrupts <- syscall.SIGINT
	select {
	case code := <-exited:
		if code != 130 {
			t.Fatalf("the second interrupt exited with %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second interrupt did not exit")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the second interrupt did not stop the session")
	}
	close(finished)
}
