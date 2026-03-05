package pool

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/bmvkrd/taiko/internal/metrics"
)

// collectResults aggregates results and streams to connectors.
func (p *Pool) collectResults() {
	intervalTicker := time.NewTicker(1 * time.Second)
	defer intervalTicker.Stop()

	if p.metricsConnector != nil { 	// Initial ticker display
		if err := p.metricsConnector.OnInterval(context.Background(), &metrics.IntervalStats{}); err != nil {
			fmt.Fprintf(p.logger, "Connector (interval) error: %v\n", err)
		}
	}

	workerSamples := make([]int, 0)

	// Interval stats
	intervalStart := time.Now()
	var intervalRequests, intervalSuccess, intervalFailure int64
	var intervalMinLatency, intervalMaxLatency time.Duration
	var intervalTotalLatency time.Duration
	intervalMinLatency = time.Duration(1<<63 - 1)

	testStart := time.Now()

	for {
		select {
		case result, ok := <-p.results:
			if !ok {
				// Channel closed, finalize
				p.finalizeStats(workerSamples, testStart)
				return
			}

			// Update counters
			p.mu.Lock()
			p.stats.TotalRequests++
			intervalRequests++

			if result.Error != nil {
				p.stats.FailedRequests++
				intervalFailure++
				errMsg := result.Error.Error()
				p.stats.Errors[errMsg]++
			} else {
				p.stats.SuccessRequests++
				intervalSuccess++
				p.stats.StatusCodes[result.StatusCode]++
			}

			// Update latency bounds
			if result.Duration < p.stats.MinLatency {
				p.stats.MinLatency = result.Duration
			}
			if result.Duration > p.stats.MaxLatency {
				p.stats.MaxLatency = result.Duration
			}
			if result.Duration < intervalMinLatency {
				intervalMinLatency = result.Duration
			}
			if result.Duration > intervalMaxLatency {
				intervalMaxLatency = result.Duration
			}
			intervalTotalLatency += result.Duration

			// Reservoir sampling for percentiles
			p.addLatencySample(result.Duration)

			p.mu.Unlock()

			// Stream to connectors
			if p.metricsConnector != nil {
				m := &metrics.RequestMetrics{
					Timestamp:  result.Timestamp,
					URI:        result.TargetURL,
					Latency:    result.Duration,
					HTTPStatus: result.StatusCode,
					Error:      result.Error,
				}

				if err := p.metricsConnector.OnRequest(context.Background(), m); err != nil {
					fmt.Fprintf(p.logger, "Connector (request) error: %v\n", err)
				}
			}

		case <-intervalTicker.C:
			// Calculate interval stats
			now := time.Now()
			elapsed := now.Sub(intervalStart).Seconds()
			actualRPS := float64(intervalRequests) / elapsed

			var avgLatency time.Duration
			if intervalRequests > 0 {
				avgLatency = intervalTotalLatency / time.Duration(intervalRequests)
			}

			intervalStats := &metrics.IntervalStats{
				Timestamp:     now,
				IntervalStart: intervalStart,
				IntervalEnd:   now,
				RequestCount:  intervalRequests,
				SuccessCount:  intervalSuccess,
				FailureCount:  intervalFailure,
				MinLatency:    intervalMinLatency,
				MaxLatency:    intervalMaxLatency,
				AvgLatency:    avgLatency,
				ActiveWorkers: int(atomic.LoadInt32(&p.activeWorkers)),
				ActualRPS:     actualRPS,
				ElapsedTime:   now.Sub(testStart),
				TotalDuration: p.duration,
			}

			// Sample worker count
			workerSamples = append(workerSamples, intervalStats.ActiveWorkers)

			// Notify reporting connector
			if p.metricsConnector != nil {
				if err := p.metricsConnector.OnInterval(context.Background(), intervalStats); err != nil {
					fmt.Fprintf(p.logger, "Connector (interval) error: %v\n", err)
				}
			}

			// Reset interval stats
			intervalStart = now
			intervalRequests = 0
			intervalSuccess = 0
			intervalFailure = 0
			intervalMinLatency = time.Duration(1<<63 - 1)
			intervalMaxLatency = 0
			intervalTotalLatency = 0
		}
	}
}

// addLatencySample adds a latency sample using reservoir sampling.
func (p *Pool) addLatencySample(latency time.Duration) {
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()

	if len(p.latencySamples) < p.sampleSize {
		// Still filling the reservoir
		p.latencySamples = append(p.latencySamples, latency)
	} else {
		// Reservoir full, randomly replace
		totalSeen := p.stats.TotalRequests
		if totalSeen > 0 {
			idx := int(totalSeen % int64(p.sampleSize))
			p.latencySamples[idx] = latency
		}
	}
}

// finalizeStats completes statistics calculation.
func (p *Pool) finalizeStats(workerSamples []int, testStart time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Calculate average latency
	if p.stats.TotalRequests > 0 {
		p.sampleMu.Lock()
		if len(p.latencySamples) > 0 {
			var total time.Duration
			for _, lat := range p.latencySamples {
				total += lat
			}
			p.stats.AvgLatency = total / time.Duration(len(p.latencySamples))
		}
		p.sampleMu.Unlock()
	}

	// Calculate percentiles from samples
	p.calculatePercentiles()

	// Calculate average workers
	if len(workerSamples) > 0 {
		sum := 0
		for _, w := range workerSamples {
			sum += w
		}
		p.stats.AvgWorkers = float64(sum) / float64(len(workerSamples))
	}

	// Calculate actual RPS
	testDuration := time.Since(testStart).Seconds()
	if testDuration > 0 {
		p.stats.ActualRPS = float64(p.stats.TotalRequests) / testDuration
	}
	p.stats.Duration = time.Since(testStart)

	// Fix min latency if no requests
	if p.stats.TotalRequests == 0 {
		p.stats.MinLatency = 0
	}

	// Notify connectors of completion
	summary := &metrics.Summary{
		TotalRequests:   p.stats.TotalRequests,
		SuccessRequests: p.stats.SuccessRequests,
		FailedRequests:  p.stats.FailedRequests,
		Duration:        p.stats.Duration,
		ActualRPS:       p.stats.ActualRPS,
		MinLatency:      p.stats.MinLatency,
		MaxLatency:      p.stats.MaxLatency,
		AvgLatency:      p.stats.AvgLatency,
		P50Latency:      p.stats.P50Latency,
		P95Latency:      p.stats.P95Latency,
		P99Latency:      p.stats.P99Latency,
		PeakWorkers:     p.stats.PeakWorkers,
		AvgWorkers:      p.stats.AvgWorkers,
		StatusCodes:     p.stats.StatusCodes,
		Errors:          p.stats.Errors,
	}

	// Notify reporting connector
	if p.metricsConnector != nil {
		if err := p.metricsConnector.OnComplete(context.Background(), summary); err != nil {
			fmt.Fprintf(p.logger, "Connector (summary) error: %v\n", err)
		}
	}
}

// calculatePercentiles computes latency percentiles from samples.
func (p *Pool) calculatePercentiles() {
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()

	if len(p.latencySamples) == 0 {
		return
	}

	// Sort samples
	sorted := make([]time.Duration, len(p.latencySamples))
	copy(sorted, p.latencySamples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	p.stats.P50Latency = sorted[len(sorted)*50/100]
	p.stats.P95Latency = sorted[len(sorted)*95/100]
	p.stats.P99Latency = sorted[len(sorted)*99/100]
}
