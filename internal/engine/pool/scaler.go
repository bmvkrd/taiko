package pool

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

const (
	// scaleUpThreshold triggers adding workers when actual RPS falls below this ratio of target RPS
	scaleUpThreshold = 0.98
	// scaleDownThreshold triggers removing workers when actual RPS exceeds this ratio of target RPS
	scaleDownThreshold = 1.03
	// baseMaxScaleUp is the base maximum workers to add per scaling cycle
	baseMaxScaleUp = 50
	// maxScaleUpCap is the absolute maximum workers to add per scaling cycle
	maxScaleUpCap = 500
	// stallImprovementThreshold is the minimum RPS improvement ratio to consider scaling effective
	stallImprovementThreshold = 0.02
)

// dynamicScaler monitors performance and adjusts worker count.
func (p *Pool) dynamicScaler(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Calibration phase
	time.Sleep(3 * time.Second)

	// Stall detection state
	var prevRPS float64
	var prevWorkerCount int
	var workersAddedLastCycle int
	var stallCycles int          // consecutive cycles with insufficient RPS improvement
	var stallStartWorkerCount int // worker count before stall-triggering scale-up
	capacityReached := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentWorkers := int(atomic.LoadInt32(&p.activeWorkers))
			currentRequests := atomic.LoadInt64(&p.requestCounter)

			// Calculate actual RPS over the interval
			requestsSinceLastCheck := currentRequests - p.lastRequestCount
			actualRPS := float64(requestsSinceLastCheck) / 2.0 // 2 second interval
			targetRPS := float64(p.totalRPS)

			p.lastRequestCount = currentRequests

			// Calculate performance ratio
			performanceRatio := actualRPS / targetRPS

			// Check for stalled scaling: we added workers but RPS didn't improve.
			// Only set capacityReached after 3 consecutive stall cycles to avoid
			// false positives from transient RPS fluctuations.
			if stallCycles > 0 {
				// Already investigating a potential stall - check if it persists
				if performanceRatio >= scaleUpThreshold {
					// RPS recovered - not a real stall
					stallCycles = 0
					capacityReached = false
				} else {
					stallCycles++
					if stallCycles >= 3 {
						// Confirmed capacity ceiling after 3 consecutive stall cycles
						workersToRemove := currentWorkers - stallStartWorkerCount
						if workersToRemove > 0 {
							fmt.Fprintf(p.logger, "WARNING: Capacity ceiling reached! Stall confirmed over %d consecutive cycles. Reverting %d workers to %d total.\n",
								stallCycles, workersToRemove, stallStartWorkerCount)
							p.removeWorkers(workersToRemove)
							currentWorkers = stallStartWorkerCount
						}
						capacityReached = true
						stallCycles = 0
					}
				}
			} else if workersAddedLastCycle > 0 && prevRPS > 0 && performanceRatio < scaleUpThreshold {
				// Only check if we're still below target - reaching target is success, not a stall
				improvement := (actualRPS - prevRPS) / prevRPS
				if improvement < stallImprovementThreshold {
					// First stall cycle detected - begin multi-cycle confirmation
					stallCycles = 1
					stallStartWorkerCount = prevWorkerCount
				} else {
					// Scaling was effective, reset capacity flag
					capacityReached = false
				}
			} else if performanceRatio >= scaleUpThreshold {
				// Target reached - clear capacity flag and stall counter
				stallCycles = 0
				capacityReached = false
			}

			// Reset tracking for this cycle
			workersAddedLastCycle = 0

			// Dynamic scaling logic
			var adjustment int

			if performanceRatio < scaleUpThreshold && !capacityReached && stallCycles == 0 {
				// Don't scale up if we already have enough workers (more than target RPS)
				// At low RPS, the rate limiter is the bottleneck, not worker count
				if currentWorkers >= p.totalRPS {
					// Skip scaling - we have enough workers, rate limiter is the constraint
				} else if currentWorkers > 0 {
					// Under-performing: need more workers
					deficit := targetRPS - actualRPS
					// Each worker should handle ~(actualRPS / currentWorkers) requests
					rpsPerWorker := actualRPS / float64(currentWorkers)
					if rpsPerWorker > 0 {
						neededWorkers := int(deficit / rpsPerWorker)
						// Scale more aggressively when far from target: maxScaleUp increases as ratio decreases
						// Also cap by target RPS to avoid overshooting for low RPS targets
						maxScaleUp := baseMaxScaleUp
						if performanceRatio > 0 {
							maxScaleUp = min(min(int(float64(baseMaxScaleUp)/performanceRatio), maxScaleUpCap), p.totalRPS)
						}
						adjustment = max(1, min(neededWorkers, maxScaleUp))
						fmt.Fprintf(p.logger, "Scaling UP: Adding %d workers (max: %d, under-performing)\n", adjustment, maxScaleUp)
					}
				}
			} else if performanceRatio > scaleDownThreshold && currentWorkers > 10 {
				// Over-performing: can reduce workers (but keep minimum)
				excess := actualRPS - targetRPS
				if currentWorkers > 0 {
					rpsPerWorker := actualRPS / float64(currentWorkers)
					if rpsPerWorker > 0 {
						excessWorkers := int(excess / rpsPerWorker)
						adjustment = -min(excessWorkers, currentWorkers/4) // Remove up to 25% workers
						if adjustment < 0 {
							fmt.Fprintf(p.logger, "Scaling DOWN: Removing %d workers (over-provisioned)\n", -adjustment)
						}
					}
				}
			}

			// Save state before applying adjustment
			prevRPS = actualRPS
			prevWorkerCount = currentWorkers

			// Apply adjustment
			if adjustment > 0 {
				p.addWorkers(ctx, adjustment)
				workersAddedLastCycle = adjustment
			} else if adjustment < 0 {
				p.removeWorkers(-adjustment)
			}

			// Track peak workers
			p.mu.Lock()
			if currentWorkers > p.stats.PeakWorkers {
				p.stats.PeakWorkers = currentWorkers
			}
			p.mu.Unlock()
		}
	}
}

// addWorkers spawns additional workers.
func (p *Pool) addWorkers(ctx context.Context, count int) {
	for i := 0; i < count; i++ {
		workerID := int(atomic.AddInt32(&p.activeWorkers, 1))
		p.workerWg.Add(1)
		go func(id int) {
			defer p.workerWg.Done()
			defer atomic.AddInt32(&p.activeWorkers, -1)
			p.worker(ctx, id)
		}(workerID)
	}
}

// removeWorkers signals workers to stop.
func (p *Pool) removeWorkers(count int) {
	for i := 0; i < count; i++ {
		select {
		case p.workerControl <- false: // Signal to stop
		default:
			return // Channel full, workers already stopping
		}
	}
}
