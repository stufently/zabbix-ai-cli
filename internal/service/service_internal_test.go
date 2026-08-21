package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunParallelBoundsWorkersAndSkipsCancelledTasks(t *testing.T) {
	started := make(chan struct{}, 12)
	release := make(chan struct{})
	tasks := make([]task, 12)
	for i := range tasks {
		tasks[i] = task{name: "task", run: func(context.Context) error {
			started <- struct{}{}
			<-release
			return nil
		}}
	}
	done := make(chan []string, 1)
	go func() { done <- runParallel(context.Background(), tasks) }()
	for range maxConcurrency {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatalf("more than %d tasks ran concurrently", maxConcurrency)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if failures := <-done; len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Int32
	cancelled := []task{
		{name: "b", run: func(context.Context) error { ran.Add(1); return nil }},
		{name: "a", run: func(context.Context) error { ran.Add(1); return nil }},
	}
	failures := runParallel(ctx, cancelled)
	if ran.Load() != 0 || len(failures) != 2 || failures[0] != "a: cancelled" || failures[1] != "b: cancelled" {
		t.Fatalf("cancelled work ran or was reported nondeterministically: ran=%d failures=%v", ran.Load(), failures)
	}
}
