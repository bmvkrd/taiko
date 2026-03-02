package metrics

import (
	"context"
	"time"
)

// RequestMetrics contains basic metrics for a single HTTP request
type RequestMetrics struct {
	Timestamp  time.Time
	URI        string
	Latency    time.Duration
	HTTPStatus int
	Error      error
}

// IntervalStats contains aggregated metrics for a time interval
type IntervalStats struct {
	Timestamp     time.Time
	IntervalStart time.Time
	IntervalEnd   time.Time
	RequestCount  int64
	SuccessCount  int64
	FailureCount  int64
	MinLatency    time.Duration
	MaxLatency    time.Duration
	AvgLatency    time.Duration
	ActiveWorkers int
	ActualRPS     float64
}

// Summary contains final test results
type Summary struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	Duration        time.Duration
	ActualRPS       float64
	MinLatency      time.Duration
	MaxLatency      time.Duration
	AvgLatency      time.Duration
	P50Latency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
	PeakWorkers     int
	AvgWorkers      float64
	StatusCodes     map[int]int64
	Errors          map[string]int64
}

// Connector defines the interface for reporting integrations
type Connector interface {
	// OnRequest is called for each individual request (real-time streaming)
	OnRequest(ctx context.Context, metrics *RequestMetrics) error

	// OnInterval is called periodically with aggregated stats
	OnInterval(ctx context.Context, stats *IntervalStats) error

	// OnComplete is called when the test finishes
	OnComplete(ctx context.Context, summary *Summary) error

	// Init initializes the connector with configuration
	Init(config map[string]string) error

	// Close cleans up resources
	Close() error
}
