package pool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bmvkrd/taiko/internal/engine"
	"golang.org/x/time/rate"
)

func TestNewPool_Validation(t *testing.T) {
	validWorkerFunc := func(_ context.Context, _ int) *engine.Result {
		return &engine.Result{Success: true}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "no targets",
			cfg:     Config{Duration: time.Second, WorkerFunc: validWorkerFunc},
			wantErr: "at least one target",
		},
		{
			name: "zero duration",
			cfg: Config{
				Targets:    []TargetMeta{{RPS: 10, Limiter: rate.NewLimiter(10, 1)}},
				WorkerFunc: validWorkerFunc,
			},
			wantErr: "duration > 0",
		},
		{
			name: "nil WorkerFunc",
			cfg: Config{
				Duration: time.Second,
				Targets:  []TargetMeta{{RPS: 10, Limiter: rate.NewLimiter(10, 1)}},
			},
			wantErr: "WorkerFunc",
		},
		{
			name: "target with zero RPS",
			cfg: Config{
				Duration:   time.Second,
				Targets:    []TargetMeta{{RPS: 0, Limiter: rate.NewLimiter(10, 1)}},
				WorkerFunc: validWorkerFunc,
			},
			wantErr: "RPS must be > 0",
		},
		{
			name: "valid config",
			cfg: Config{
				Duration:   time.Second,
				Targets:    []TargetMeta{{RPS: 10, Limiter: rate.NewLimiter(10, 1)}},
				WorkerFunc: validWorkerFunc,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsStr(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("expected non-nil pool")
			}
		})
	}
}

func TestPool_RunCompletesAndReturnsStats(t *testing.T) {
	var callCount int64

	workerFunc := func(_ context.Context, targetIndex int) *engine.Result {
		atomic.AddInt64(&callCount, 1)
		return &engine.Result{
			Timestamp:  time.Now(),
			Duration:   5 * time.Millisecond,
			StatusCode: 200,
			Success:    true,
			TargetURL:  "http://test",
		}
	}

	p, err := New(Config{
		Duration: 2 * time.Second,
		Targets: []TargetMeta{
			{RPS: 50, Limiter: rate.NewLimiter(50, 5)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if stats.TotalRequests == 0 {
		t.Fatal("expected TotalRequests > 0")
	}
	if stats.SuccessRequests == 0 {
		t.Fatal("expected SuccessRequests > 0")
	}
	if stats.FailedRequests != 0 {
		t.Fatalf("expected 0 FailedRequests, got %d", stats.FailedRequests)
	}
	if stats.Duration <= 0 {
		t.Fatal("expected Duration > 0")
	}
	if stats.MinLatency <= 0 {
		t.Fatal("expected MinLatency > 0")
	}
	if stats.MaxLatency <= 0 {
		t.Fatal("expected MaxLatency > 0")
	}
	if stats.AvgLatency <= 0 {
		t.Fatal("expected AvgLatency > 0")
	}

	code200, ok := stats.StatusCodes[200]
	if !ok || code200 == 0 {
		t.Fatal("expected status code 200 in StatusCodes")
	}
}

func TestPool_FailedRequests(t *testing.T) {
	workerFunc := func(_ context.Context, _ int) *engine.Result {
		return &engine.Result{
			Timestamp: time.Now(),
			Duration:  1 * time.Millisecond,
			Error:     errMock,
		}
	}

	p, err := New(Config{
		Duration: 1 * time.Second,
		Targets: []TargetMeta{
			{RPS: 20, Limiter: rate.NewLimiter(20, 2)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if stats.FailedRequests == 0 {
		t.Fatal("expected FailedRequests > 0")
	}
	if stats.SuccessRequests != 0 {
		t.Fatalf("expected 0 SuccessRequests, got %d", stats.SuccessRequests)
	}
	if _, ok := stats.Errors["mock error"]; !ok {
		t.Fatal("expected 'mock error' in Errors map")
	}
}

func TestPool_MultiTargetWeighting(t *testing.T) {
	var target0Count, target1Count int64

	workerFunc := func(_ context.Context, targetIndex int) *engine.Result {
		if targetIndex == 0 {
			atomic.AddInt64(&target0Count, 1)
		} else {
			atomic.AddInt64(&target1Count, 1)
		}
		return &engine.Result{
			Timestamp:  time.Now(),
			Duration:   1 * time.Millisecond,
			StatusCode: 200,
			Success:    true,
		}
	}

	// Target 0 has 3x the RPS of target 1
	p, err := New(Config{
		Duration: 2 * time.Second,
		Targets: []TargetMeta{
			{RPS: 60, Limiter: rate.NewLimiter(60, 5)},
			{RPS: 20, Limiter: rate.NewLimiter(20, 2)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	t0 := atomic.LoadInt64(&target0Count)
	t1 := atomic.LoadInt64(&target1Count)

	if t0 == 0 || t1 == 0 {
		t.Fatalf("expected both targets to receive requests, got target0=%d target1=%d", t0, t1)
	}

	// Target 0 should get roughly 3x the requests of target 1
	// Allow wide tolerance (1.5x to 6x) since rate limiters and short duration add variance
	ratio := float64(t0) / float64(t1)
	if ratio < 1.5 || ratio > 6.0 {
		t.Fatalf("expected target0/target1 ratio ~3.0, got %.2f (target0=%d, target1=%d)", ratio, t0, t1)
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	workerFunc := func(_ context.Context, _ int) *engine.Result {
		return &engine.Result{
			Timestamp:  time.Now(),
			Duration:   1 * time.Millisecond,
			StatusCode: 200,
			Success:    true,
		}
	}

	p, err := New(Config{
		Duration: 30 * time.Second, // Long duration
		Targets: []TargetMeta{
			{RPS: 100, Limiter: rate.NewLimiter(100, 10)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	stats, err := p.Run(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stats.TotalRequests == 0 {
		t.Fatal("expected some requests before cancellation")
	}
	// Should finish much sooner than 30 seconds
	if elapsed > 5*time.Second {
		t.Fatalf("expected pool to stop within ~1s of context cancel, took %v", elapsed)
	}
}

func TestPool_Percentiles(t *testing.T) {
	workerFunc := func(_ context.Context, _ int) *engine.Result {
		return &engine.Result{
			Timestamp:  time.Now(),
			Duration:   10 * time.Millisecond,
			StatusCode: 200,
			Success:    true,
		}
	}

	p, err := New(Config{
		Duration: 2 * time.Second,
		Targets: []TargetMeta{
			{RPS: 50, Limiter: rate.NewLimiter(50, 5)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if stats.P50Latency <= 0 {
		t.Fatal("expected P50Latency > 0")
	}
	if stats.P95Latency <= 0 {
		t.Fatal("expected P95Latency > 0")
	}
	if stats.P99Latency <= 0 {
		t.Fatal("expected P99Latency > 0")
	}
	// P99 >= P95 >= P50 (with uniform latency they should be equal, but allow >=)
	if stats.P99Latency < stats.P95Latency {
		t.Fatalf("expected P99 >= P95, got P99=%v P95=%v", stats.P99Latency, stats.P95Latency)
	}
	if stats.P95Latency < stats.P50Latency {
		t.Fatalf("expected P95 >= P50, got P95=%v P50=%v", stats.P95Latency, stats.P50Latency)
	}
}

func TestPool_Close(t *testing.T) {
	workerFunc := func(_ context.Context, _ int) *engine.Result {
		return &engine.Result{Success: true}
	}

	p, err := New(Config{
		Duration: time.Second,
		Targets: []TargetMeta{
			{RPS: 10, Limiter: rate.NewLimiter(10, 1)},
		},
		WorkerFunc: workerFunc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close without running should not panic
	if err := p.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestSelectTarget_SingleTarget(t *testing.T) {
	p := &Pool{
		totalRPS: 10,
		targets: []TargetMeta{
			{RPS: 10, Limiter: rate.NewLimiter(10, 1)},
		},
	}

	for i := 0; i < 100; i++ {
		idx := p.selectTarget()
		if idx != 0 {
			t.Fatalf("expected target index 0, got %d", idx)
		}
	}
}

func TestSelectTarget_MultipleTargets(t *testing.T) {
	p := &Pool{
		totalRPS: 100,
		targets: []TargetMeta{
			{RPS: 70},
			{RPS: 20},
			{RPS: 10},
		},
	}

	counts := make([]int, 3)
	iterations := 10000
	for i := 0; i < iterations; i++ {
		idx := p.selectTarget()
		counts[idx]++
	}

	// With 70/20/10 split, target 0 should get ~70%, target 1 ~20%, target 2 ~10%
	// Allow 5% tolerance
	ratio0 := float64(counts[0]) / float64(iterations)
	ratio1 := float64(counts[1]) / float64(iterations)
	ratio2 := float64(counts[2]) / float64(iterations)

	if ratio0 < 0.65 || ratio0 > 0.75 {
		t.Fatalf("expected target 0 ~70%%, got %.1f%%", ratio0*100)
	}
	if ratio1 < 0.15 || ratio1 > 0.25 {
		t.Fatalf("expected target 1 ~20%%, got %.1f%%", ratio1*100)
	}
	if ratio2 < 0.05 || ratio2 > 0.15 {
		t.Fatalf("expected target 2 ~10%%, got %.1f%%", ratio2*100)
	}
}

// --- helpers ---

type mockError struct{}

func (e *mockError) Error() string { return "mock error" }

var errMock = &mockError{}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
