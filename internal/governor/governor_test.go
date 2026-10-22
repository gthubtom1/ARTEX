package governor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunAppliesTimeout(t *testing.T) {
	g := New(Limits{CmdTimeout: 50 * time.Millisecond})
	_, _, err := g.Run(context.Background(), func(c context.Context) ([]byte, error) {
		select {
		case <-time.After(2 * time.Second):
			return []byte("late"), nil
		case <-c.Done():
			return nil, c.Err()
		}
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

func TestRunCapsOutput(t *testing.T) {
	g := New(Limits{OutputCap: 10})
	out, truncated, err := g.Run(context.Background(), func(context.Context) ([]byte, error) {
		return []byte("0123456789ABCDEF"), nil
	})
	if err != nil || !truncated {
		t.Fatalf("truncated=%v err=%v", truncated, err)
	}
	if string(out) != "0123456789" {
		t.Fatalf("out=%q", out)
	}
}

func TestParallelLimit(t *testing.T) {
	g := New(Limits{MaxParallel: 2, CmdTimeout: 2 * time.Second})
	var mu sync.Mutex
	cur, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = g.Run(context.Background(), func(context.Context) ([]byte, error) {
				mu.Lock()
				cur++
				if cur > peak {
					peak = cur
				}
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				cur--
				mu.Unlock()
				return nil, nil
			})
		}()
	}
	wg.Wait()
	if peak > 2 {
		t.Fatalf("peak concurrency=%d > 2", peak)
	}
}

func TestAcquireCancel(t *testing.T) {
	g := New(Limits{MaxParallel: 1})
	rel, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx); err == nil {
		t.Fatal("second acquire must block until ctx done")
	}
	rel()
	if _, err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("after release: %v", err)
	}
}
