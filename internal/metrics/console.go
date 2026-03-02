package metrics

import (
	"context"
	"fmt"
)

type ConsoleConnector struct{}

func NewConsoleConnector() Connector {
	return &ConsoleConnector{}
}

func (c *ConsoleConnector) Init(config map[string]string) error {
	return nil
}

func (c *ConsoleConnector) OnRequest(ctx context.Context, metrics *RequestMetrics) error {
	return nil
}

func (c *ConsoleConnector) OnInterval(ctx context.Context, stats *IntervalStats) error {
	fmt.Printf("[Interval] Requests: %d | Success: %d | Failed: %d | RPS: %.2f | Workers: %d | Latency (avg): %v\n",
		stats.RequestCount, stats.SuccessCount, stats.FailureCount,
		stats.ActualRPS, stats.ActiveWorkers, stats.AvgLatency)
	return nil
}

func (c *ConsoleConnector) OnComplete(ctx context.Context, summary *Summary) error {
	fmt.Println("\n=== Load Test Summary ===")
	fmt.Printf("Total Requests:  %d\n", summary.TotalRequests)
	fmt.Printf("Success:         %d\n", summary.SuccessRequests)
	fmt.Printf("Failed:          %d\n", summary.FailedRequests)
	fmt.Printf("Duration:        %v\n", summary.Duration)
	fmt.Printf("Actual RPS:      %.2f\n", summary.ActualRPS)
	fmt.Printf("Peak Workers:    %d\n", summary.PeakWorkers)
	fmt.Printf("Avg Workers:     %.2f\n", summary.AvgWorkers)
	fmt.Println("\nLatency:")
	fmt.Printf("  Min: %v\n", summary.MinLatency)
	fmt.Printf("  Max: %v\n", summary.MaxLatency)
	fmt.Printf("  Avg: %v\n", summary.AvgLatency)
	fmt.Printf("  P50: %v\n", summary.P50Latency)
	fmt.Printf("  P95: %v\n", summary.P95Latency)
	fmt.Printf("  P99: %v\n", summary.P99Latency)
	if len(summary.StatusCodes) > 0 {
		fmt.Println("\nStatus Codes:")
		for code, count := range summary.StatusCodes {
			fmt.Printf("  %d: %d\n", code, count)
		}
	}
	if len(summary.Errors) > 0 {
		fmt.Println("\nErrors:")
		for errType, count := range summary.Errors {
			fmt.Printf("  %s: %d\n", errType, count)
		}
	}
	return nil
}

func (c *ConsoleConnector) Close() error {
	return nil
}
