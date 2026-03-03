package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const progressBarWidth = 40

type ConsoleConnector struct {
	totalRequests int64
	totalSuccess  int64
	totalFailure  int64
	intervals     int
}

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
	c.totalRequests += stats.RequestCount
	c.totalSuccess += stats.SuccessCount
	c.totalFailure += stats.FailureCount

	statsLine := fmt.Sprintf("RPS: %f | Workers: %d | Requests: %d (Success: %d / Failed: %d)",
		stats.ActualRPS, stats.ActiveWorkers, c.totalRequests, c.totalSuccess, c.totalFailure)

	progressLine := buildProgressBar(stats.ElapsedTime, stats.TotalDuration)

	if c.intervals > 0 {
		// Move cursor up 2 lines to overwrite previous output
		fmt.Print("\033[2A")
	}

	fmt.Printf("\033[2K%s\n\033[2K%s\n", statsLine, progressLine)

	c.intervals++
	return nil
}

func (c *ConsoleConnector) OnComplete(ctx context.Context, summary *Summary) error {
	if c.intervals > 0 {
		// Clear the two live lines before printing the summary
		fmt.Print("\033[2A\033[2K\033[1B\033[2K\033[1A")
	}

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

func buildProgressBar(elapsed, total time.Duration) string {
	var ratio float64
	if total > 0 {
		ratio = float64(elapsed) / float64(total)
		if ratio > 1 {
			ratio = 1
		}
	}

	filled := int(ratio * progressBarWidth)
	if filled > progressBarWidth {
		filled = progressBarWidth
	}
	empty := progressBarWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	elapsedSec := int(elapsed.Seconds())
	totalSec := int(total.Seconds())

	return fmt.Sprintf("%s %3.0f%%  %ds / %ds", bar, ratio*100, elapsedSec, totalSec)
}
