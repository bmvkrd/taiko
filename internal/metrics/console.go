package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bmvkrd/livelog"
)

const progressBarWidth = 40

type ConsoleConnector struct {
	display       *livelog.Display
	totalRequests int64
	totalSuccess  int64
	totalFailure  int64
}

func NewConsoleConnector() Connector {
	return &ConsoleConnector{display: globalDisplay.Load()}
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

	if c.display != nil {
		c.display.SetLive([]string{statsLine, progressLine})
	} else {
		fmt.Println(statsLine)
		fmt.Println(progressLine)
	}
	return nil
}

func (c *ConsoleConnector) OnComplete(ctx context.Context, summary *Summary) error {
	if c.display != nil {
		c.display.ClearLive()
		c.logSummary(summary)
	} else {
		c.printSummary(summary)
	}
	return nil
}

func (c *ConsoleConnector) logSummary(summary *Summary) {
	c.display.Log("\n=== Load Test Summary ===")
	c.display.Logf("Total Requests:  %d", summary.TotalRequests)
	c.display.Logf("Success:         %d", summary.SuccessRequests)
	c.display.Logf("Failed:          %d", summary.FailedRequests)
	c.display.Logf("Duration:        %v", summary.Duration)
	c.display.Logf("Actual RPS:      %.2f", summary.ActualRPS)
	c.display.Logf("Peak Workers:    %d", summary.PeakWorkers)
	c.display.Logf("Avg Workers:     %.2f", summary.AvgWorkers)
	c.display.Log("\nLatency:")
	c.display.Logf("  Min: %v", summary.MinLatency)
	c.display.Logf("  Max: %v", summary.MaxLatency)
	c.display.Logf("  Avg: %v", summary.AvgLatency)
	c.display.Logf("  P50: %v", summary.P50Latency)
	c.display.Logf("  P95: %v", summary.P95Latency)
	c.display.Logf("  P99: %v", summary.P99Latency)
	if len(summary.StatusCodes) > 0 {
		c.display.Log("\nStatus Codes:")
		for code, count := range summary.StatusCodes {
			c.display.Logf("  %d: %d", code, count)
		}
	}
	if len(summary.Errors) > 0 {
		c.display.Log("\nErrors:")
		for errType, count := range summary.Errors {
			c.display.Logf("  %s: %d", errType, count)
		}
	}
}

func (c *ConsoleConnector) printSummary(summary *Summary) {
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
