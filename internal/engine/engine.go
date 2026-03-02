package engine

import (
	"context"
	"time"
)

// Result holds metrics for a single request
type Result struct {
	Duration   time.Duration
	StatusCode int // Protocol-specific status (HTTP status, gRPC code, etc.)
	Success    bool
	Error      error
	Timestamp  time.Time
	TargetURL  string // URL of the target (for multi-target support)
}

// Stats holds aggregated test statistics
type Stats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	Duration        time.Duration
	MinLatency      time.Duration
	MaxLatency      time.Duration
	AvgLatency      time.Duration

	// Percentiles (approximated)
	P50Latency time.Duration
	P95Latency time.Duration
	P99Latency time.Duration

	// Protocol-specific status codes (HTTP status codes, gRPC codes, etc.)
	StatusCodes map[int]int64

	// Errors
	Errors map[string]int64

	// Auto-scaling metrics
	PeakWorkers int
	AvgWorkers  float64
	ActualRPS   float64
}

// Engine is the interface for load test engines
type Engine interface {
	// Run executes the load test and returns statistics
	Run(ctx context.Context) (*Stats, error)
	// Close releases resources (e.g., metrics connector)
	Close() error
}
