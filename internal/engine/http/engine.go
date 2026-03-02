package http

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/generator"
	"github.com/bmvkrd/taiko/internal/metrics"
	"golang.org/x/time/rate"
)

func init() {
	engine.Register("http", NewHTTPEngine)
}

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

// targetState holds per-target state including its rate limiter
type targetState struct {
	config  *config.HTTPTargetConfig
	limiter *rate.Limiter
	// Pre-extracted for efficiency
	url     string
	method  string
	body    string
	headers map[string]string
}

// HTTPEngine is the HTTP load generation engine
type HTTPEngine struct {
	// Load test parameters (from config)
	duration time.Duration
	totalRPS int // Sum of all target RPS (for scaling)

	// Multiple targets with individual rate limiters
	targets []*targetState

	// Variable generators for substitution
	generators map[string]generator.Generator
	varPattern *regexp.Regexp

	metricsConnector metrics.Connector

	client  *http.Client
	results chan *engine.Result
	stats   *engine.Stats
	mu      sync.Mutex

	// Dynamic scaling (always enabled)
	activeWorkers    int32
	requestCounter   int64
	lastRequestCount int64
	workerControl    chan bool
	workerWg         sync.WaitGroup // Tracks all workers for graceful shutdown

	// Percentile approximation (reservoir sampling)
	latencySamples []time.Duration
	sampleSize     int
	sampleMu       sync.Mutex
}

// NewHTTPEngine creates a new HTTP load generation engine
func NewHTTPEngine(cfg *config.Config) (engine.Engine, error) {
	// Validate targets
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("http engine requires at least one target")
	}

	// Parse load duration
	duration, err := time.ParseDuration(cfg.Load.Duration)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	// Build target states with individual rate limiters
	var targets []*targetState
	var totalRPS int
	var maxTimeout time.Duration = 30 * time.Second

	for i, t := range cfg.Targets {
		httpTarget, ok := t.(*config.HTTPTargetConfig)
		if !ok {
			return nil, fmt.Errorf("target[%d]: http engine requires HTTP target configuration", i)
		}

		if httpTarget.URL == "" {
			return nil, fmt.Errorf("target[%d]: http engine requires 'url' config", i)
		}

		if httpTarget.RPS <= 0 {
			return nil, fmt.Errorf("target[%d]: http engine requires 'rps' > 0", i)
		}

		method := httpTarget.Method
		if method == "" {
			method = "GET"
		}

		// Parse request timeout from target config
		timeout := 30 * time.Second
		if httpTarget.Timeout != "" {
			if t, err := time.ParseDuration(httpTarget.Timeout); err == nil {
				timeout = t
			}
		}
		if timeout > maxTimeout {
			maxTimeout = timeout
		}

		// Create rate limiter for this target
		burst := httpTarget.GetBurst()
		limiter := rate.NewLimiter(rate.Limit(httpTarget.RPS), burst)

		ts := &targetState{
			config:  httpTarget,
			limiter: limiter,
			url:     httpTarget.URL,
			method:  method,
			body:    httpTarget.Body,
			headers: httpTarget.Headers,
		}
		targets = append(targets, ts)
		totalRPS += httpTarget.RPS
	}

	// Initialize metrics connector from config
	metricsType := cfg.Metrics.Type
	if metricsType == "" {
		metricsType = "console"
	}
	metricsConnector, err := metrics.Get(metricsType)
	if err != nil {
		return nil, fmt.Errorf("metrics connector error: %w", err)
	}
	if err := metricsConnector.Init(cfg.Metrics.Config); err != nil {
		return nil, fmt.Errorf("metrics connector init error: %w", err)
	}

	// Initialize variable generators
	generators := make(map[string]generator.Generator)
	for _, v := range cfg.Variables {
		gen, err := createGenerator(v)
		if err != nil {
			return nil, fmt.Errorf("failed to create generator for variable '%s': %w", v.Name, err)
		}
		generators[v.Name] = gen
	}

	return &HTTPEngine{
		duration:         duration,
		totalRPS:         totalRPS,
		targets:          targets,
		generators:       generators,
		varPattern:       regexp.MustCompile(`\{\{(\w+)\}\}`),
		metricsConnector: metricsConnector,
		client: &http.Client{
			Timeout: maxTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 1000,
				MaxConnsPerHost:     2000,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
				DisableCompression:  false,
				ForceAttemptHTTP2:   false, // Forcing HTTP/1.1
			},
		},
		results:        make(chan *engine.Result, 10000),
		workerControl:  make(chan bool, 1000),
		sampleSize:     10000, // Keep 10k samples for percentiles
		latencySamples: make([]time.Duration, 0, 10000),
		stats: &engine.Stats{
			StatusCodes: make(map[int]int64),
			Errors:      make(map[string]int64),
			MinLatency:  time.Duration(1<<63 - 1),
		},
	}, nil
}

// Run executes the load test with dynamic scaling
func (e *HTTPEngine) Run(ctx context.Context) (*engine.Stats, error) {
	fmt.Println("Starting load test...")

	// Total RPS must be > 0 (validated during construction)
	if e.totalRPS <= 0 {
		return nil, fmt.Errorf("total RPS must be > 0")
	}

	// Create context with timeout
	testCtx, cancel := context.WithTimeout(ctx, e.duration)
	defer cancel()

	// Start result collector
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.collectResults()
	}()

	// Always start dynamic scaler
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.dynamicScaler(testCtx)
	}()

	// Start with initial workers: match target RPS for low values, cap at 10 otherwise
	initialWorkers := min(e.totalRPS, 10)
	for i := 0; i < initialWorkers; i++ {
		atomic.AddInt32(&e.activeWorkers, 1)
		e.workerWg.Add(1)
		go func(workerID int) {
			defer e.workerWg.Done()
			defer atomic.AddInt32(&e.activeWorkers, -1)
			e.worker(testCtx, workerID)
		}(i)
	}

	// Wait for test to complete
	<-testCtx.Done()

	// Signal all workers to stop
	close(e.workerControl)

	// Wait for all workers (initial + dynamically added) to finish
	e.workerWg.Wait()
	close(e.results)

	// Wait for collectors
	wg.Wait()

	// Calculate final stats
	e.calculateStats()

	fmt.Println("\nLoad test completed!")
	return e.stats, nil
}

// dynamicScaler monitors performance and adjusts worker count
func (e *HTTPEngine) dynamicScaler(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
	defer ticker.Stop()

	fmt.Println("Dynamic scaling enabled")

	// Calibration phase
	time.Sleep(3 * time.Second) // Let initial workers stabilize

	workerSamples := make([]int, 0)

	// Stall detection state
	var prevRPS float64
	var prevWorkerCount int
	var workersAddedLastCycle int
	capacityReached := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentWorkers := int(atomic.LoadInt32(&e.activeWorkers))
			currentRequests := atomic.LoadInt64(&e.requestCounter)

			// Calculate actual RPS over the interval
			requestsSinceLastCheck := currentRequests - e.lastRequestCount
			actualRPS := float64(requestsSinceLastCheck) / 2.0 // 2 second interval
			targetRPS := float64(e.totalRPS)

			e.lastRequestCount = currentRequests

			// Track worker count for averaging
			workerSamples = append(workerSamples, currentWorkers)

			// Calculate performance ratio
			performanceRatio := actualRPS / targetRPS

			// Check for stalled scaling: we added workers but RPS didn't improve
			// Only check if we're still below target - reaching target is success, not a stall
			if workersAddedLastCycle > 0 && prevRPS > 0 && performanceRatio < scaleUpThreshold {
				improvement := (actualRPS - prevRPS) / prevRPS
				if improvement < stallImprovementThreshold {
					// Scaling didn't help - revert to previous worker count
					fmt.Printf("WARNING: Capacity ceiling reached! Added %d workers but RPS improved only %.1f%%. Reverting to %d workers.\n",
						workersAddedLastCycle, improvement*100, prevWorkerCount)
					workersToRemove := currentWorkers - prevWorkerCount
					if workersToRemove > 0 {
						e.removeWorkers(workersToRemove)
						currentWorkers = prevWorkerCount
					}
					capacityReached = true
				} else {
					// Scaling was effective, reset capacity flag
					capacityReached = false
				}
			} else if performanceRatio >= scaleUpThreshold {
				// Target reached - clear capacity flag
				capacityReached = false
			}

			// Log status
			if capacityReached {
				fmt.Printf("Workers: %d | Target: %.0f RPS | Actual: %.0f RPS | Ratio: %.2f | CAPACITY LIMIT REACHED\n",
					currentWorkers, targetRPS, actualRPS, performanceRatio)
			} else {
				fmt.Printf("Workers: %d | Target: %.0f RPS | Actual: %.0f RPS | Ratio: %.2f\n",
					currentWorkers, targetRPS, actualRPS, performanceRatio)
			}

			// Reset tracking for this cycle
			workersAddedLastCycle = 0

			// Dynamic scaling logic
			var adjustment int

			if performanceRatio < scaleUpThreshold && !capacityReached {
				// Don't scale up if we already have enough workers (more than target RPS)
				// At low RPS, the rate limiter is the bottleneck, not worker count
				if currentWorkers >= e.totalRPS {
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
							maxScaleUp = min(min(int(float64(baseMaxScaleUp)/performanceRatio), maxScaleUpCap), e.totalRPS)
						}
						adjustment = max(1, min(neededWorkers, maxScaleUp))
						fmt.Printf("   Scaling UP: Adding %d workers (max: %d, under-performing)\n", adjustment, maxScaleUp)
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
							fmt.Printf("   Scaling DOWN: Removing %d workers (over-provisioned)\n", -adjustment)
						}
					}
				}
			}

			// Save state before applying adjustment
			prevRPS = actualRPS
			prevWorkerCount = currentWorkers

			// Apply adjustment
			if adjustment > 0 {
				e.addWorkers(ctx, adjustment)
				workersAddedLastCycle = adjustment
			} else if adjustment < 0 {
				e.removeWorkers(-adjustment)
			}

			// Track peak workers
			e.mu.Lock()
			if currentWorkers > e.stats.PeakWorkers {
				e.stats.PeakWorkers = currentWorkers
			}
			e.mu.Unlock()
		}
	}
}

// addWorkers spawns additional workers
func (e *HTTPEngine) addWorkers(ctx context.Context, count int) {
	for i := 0; i < count; i++ {
		atomic.AddInt32(&e.activeWorkers, 1)
		e.workerWg.Add(1)
		go func(workerID int) {
			defer e.workerWg.Done()
			defer atomic.AddInt32(&e.activeWorkers, -1)
			e.worker(ctx, workerID)
		}(int(atomic.LoadInt32(&e.activeWorkers)))
	}
}

// removeWorkers signals workers to stop
func (e *HTTPEngine) removeWorkers(count int) {
	for i := 0; i < count; i++ {
		select {
		case e.workerControl <- false: // Signal to stop
		default:
			return // Channel full, workers already stopping
		}
	}
}

// selectTarget selects a target weighted by RPS
func (e *HTTPEngine) selectTarget() *targetState {
	r := rand.Intn(e.totalRPS)
	cumulative := 0
	for _, t := range e.targets {
		cumulative += t.config.RPS
		if r < cumulative {
			return t
		}
	}
	return e.targets[len(e.targets)-1]
}

// worker is a goroutine that generates load
func (e *HTTPEngine) worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case shouldStop, ok := <-e.workerControl:
			if !ok || !shouldStop {
				return // Stop signal or channel closed
			}
		default:
			// Select target weighted by RPS
			target := e.selectTarget()

			// Wait for this target's rate limiter token
			if err := target.limiter.Wait(ctx); err != nil {
				return
			}

			// Make request to selected target
			result := e.makeRequestToTarget(target)
			atomic.AddInt64(&e.requestCounter, 1)

			// Send result to collector
			select {
			case e.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// makeRequestToTarget performs a single HTTP request to the specified target
func (e *HTTPEngine) makeRequestToTarget(target *targetState) *engine.Result {
	// Generate values once for this request
	values := e.generateValues()

	// Substitute variables in URL and body using the same values
	url := e.substituteVariables(target.url, values)
	body := e.substituteVariables(target.body, values)

	result := &engine.Result{
		Timestamp: time.Now(),
		TargetURL: url,
	}

	start := time.Now()

	// Create request
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(target.method, url, bodyReader)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	// Add headers
	for key, value := range target.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := e.client.Do(req)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		return result
	}
	defer resp.Body.Close()

	// Read and discard body (important for connection reuse)
	_, _ = io.Copy(io.Discard, resp.Body)

	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 400
	return result
}

// addLatencySample adds a latency sample using reservoir sampling
func (e *HTTPEngine) addLatencySample(latency time.Duration) {
	e.sampleMu.Lock()
	defer e.sampleMu.Unlock()

	if len(e.latencySamples) < e.sampleSize {
		// Still filling the reservoir
		e.latencySamples = append(e.latencySamples, latency)
	} else {
		// Reservoir full, randomly replace
		// Algorithm R: probability = sampleSize / totalSeen
		totalSeen := e.stats.TotalRequests
		if totalSeen > 0 {
			// Simple random replacement (not perfectly uniform but good enough)
			idx := int(totalSeen % int64(e.sampleSize))
			e.latencySamples[idx] = latency
		}
	}
}

// finalizeStats completes statistics calculation
func (e *HTTPEngine) finalizeStats(workerSamples []int, testStart time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Calculate average latency
	if e.stats.TotalRequests > 0 {
		// We track avg through samples
		e.sampleMu.Lock()
		if len(e.latencySamples) > 0 {
			var total time.Duration
			for _, lat := range e.latencySamples {
				total += lat
			}
			e.stats.AvgLatency = total / time.Duration(len(e.latencySamples))
		}
		e.sampleMu.Unlock()
	}

	// Calculate percentiles from samples
	e.calculatePercentiles()

	// Calculate average workers
	if len(workerSamples) > 0 {
		sum := 0
		for _, w := range workerSamples {
			sum += w
		}
		e.stats.AvgWorkers = float64(sum) / float64(len(workerSamples))
	}

	// Calculate actual RPS
	testDuration := time.Since(testStart).Seconds()
	if testDuration > 0 {
		e.stats.ActualRPS = float64(e.stats.TotalRequests) / testDuration
	}
	e.stats.Duration = time.Since(testStart)

	// Fix min latency if no requests
	if e.stats.TotalRequests == 0 {
		e.stats.MinLatency = 0
	}

	// Notify connectors of completion
	summary := &metrics.Summary{
		TotalRequests:   e.stats.TotalRequests,
		SuccessRequests: e.stats.SuccessRequests,
		FailedRequests:  e.stats.FailedRequests,
		Duration:        e.stats.Duration,
		ActualRPS:       e.stats.ActualRPS,
		MinLatency:      e.stats.MinLatency,
		MaxLatency:      e.stats.MaxLatency,
		AvgLatency:      e.stats.AvgLatency,
		P50Latency:      e.stats.P50Latency,
		P95Latency:      e.stats.P95Latency,
		P99Latency:      e.stats.P99Latency,
		PeakWorkers:     e.stats.PeakWorkers,
		AvgWorkers:      e.stats.AvgWorkers,
		StatusCodes:     e.stats.StatusCodes,
		Errors:          e.stats.Errors,
	}

	// Notify reporting connector
	if e.metricsConnector != nil {
		if err := e.metricsConnector.OnComplete(context.Background(), summary); err != nil {
			fmt.Printf("Connector (summary) error: %v\n", err)
		}
	}
}

// collectResults aggregates results and streams to connectors
func (e *HTTPEngine) collectResults() {
	intervalTicker := time.NewTicker(5 * time.Second)
	defer intervalTicker.Stop()

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
		case result, ok := <-e.results:
			if !ok {
				// Channel closed, finalize
				e.finalizeStats(workerSamples, testStart)
				return
			}

			// Update counters
			e.mu.Lock()
			e.stats.TotalRequests++
			intervalRequests++

			if result.Error != nil {
				e.stats.FailedRequests++
				intervalFailure++
				errMsg := result.Error.Error()
				e.stats.Errors[errMsg]++
			} else {
				e.stats.SuccessRequests++
				intervalSuccess++
				e.stats.StatusCodes[result.StatusCode]++
			}

			// Update latency bounds
			if result.Duration < e.stats.MinLatency {
				e.stats.MinLatency = result.Duration
			}
			if result.Duration > e.stats.MaxLatency {
				e.stats.MaxLatency = result.Duration
			}
			if result.Duration < intervalMinLatency {
				intervalMinLatency = result.Duration
			}
			if result.Duration > intervalMaxLatency {
				intervalMaxLatency = result.Duration
			}
			intervalTotalLatency += result.Duration

			// Reservoir sampling for percentiles
			e.addLatencySample(result.Duration)

			e.mu.Unlock()

			// Stream to connectors
			if e.metricsConnector != nil {
				metrics := &metrics.RequestMetrics{
					Timestamp:  result.Timestamp,
					URI:        result.TargetURL,
					Latency:    result.Duration,
					HTTPStatus: result.StatusCode,
					Error:      result.Error,
				}

				if err := e.metricsConnector.OnRequest(context.Background(), metrics); err != nil {
					fmt.Printf("Connector (request) error: %v\n", err)
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
				ActiveWorkers: int(atomic.LoadInt32(&e.activeWorkers)),
				ActualRPS:     actualRPS,
			}

			// Sample worker count
			workerSamples = append(workerSamples, intervalStats.ActiveWorkers)

			// Notify reporting connector
			if e.metricsConnector != nil {
				if err := e.metricsConnector.OnInterval(context.Background(), intervalStats); err != nil {
					fmt.Printf("Connector (interval) error: %v\n", err)
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

// calculateStats computes final statistics (deprecated, kept for compatibility)
func (e *HTTPEngine) calculateStats() {
	// Stats are now calculated in finalizeStats
	// This method is kept for backward compatibility
}

// calculatePercentiles computes latency percentiles from samples
func (e *HTTPEngine) calculatePercentiles() {
	e.sampleMu.Lock()
	defer e.sampleMu.Unlock()

	if len(e.latencySamples) == 0 {
		return
	}

	// Sort samples
	sorted := make([]time.Duration, len(e.latencySamples))
	copy(sorted, e.latencySamples)

	// Simple bubble sort (good enough for 10k samples)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	e.stats.P50Latency = sorted[len(sorted)*50/100]
	e.stats.P95Latency = sorted[len(sorted)*95/100]
	e.stats.P99Latency = sorted[len(sorted)*99/100]
}

// Close releases engine resources
func (e *HTTPEngine) Close() error {
	if e.metricsConnector != nil {
		return e.metricsConnector.Close()
	}
	return nil
}

// createGenerator creates a generator from a variable configuration
func createGenerator(v config.Variable) (generator.Generator, error) {
	switch v.Type {
	case string(generator.IntRangeType):
		mode := generator.Mode(v.Generator["mode"].(string))
		min := toInt(v.Generator["min"])
		max := toInt(v.Generator["max"])
		return generator.NewIntRangeGenerator(min, max, mode)
	case string(generator.IntSetType):
		mode := generator.Mode(v.Generator["mode"].(string))
		rawValues := v.Generator["values"]
		values := toIntSlice(rawValues)
		return generator.NewIntSetGenerator(values, mode)
	case string(generator.UUIDType):
		return generator.NewUUIDGenerator(), nil
	default:
		return nil, fmt.Errorf("unknown generator type: %s", v.Type)
	}
}

// toInt converts various numeric types to int
func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

// toIntSlice converts various slice types to []int
func toIntSlice(v any) []int {
	switch val := v.(type) {
	case []int:
		return val
	case []any:
		result := make([]int, len(val))
		for i, item := range val {
			result[i] = toInt(item)
		}
		return result
	default:
		return nil
	}
}

// generateValues generates a value for each variable generator
func (e *HTTPEngine) generateValues() map[string]string {
	values := make(map[string]string, len(e.generators))
	for name, gen := range e.generators {
		values[name] = fmt.Sprintf("%v", gen.Next())
	}
	return values
}

// substituteVariables replaces {{var}} placeholders with pre-generated values
func (e *HTTPEngine) substituteVariables(s string, values map[string]string) string {
	if len(values) == 0 {
		return s
	}
	return e.varPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name from {{name}}
		varName := match[2 : len(match)-2]
		if val, ok := values[varName]; ok {
			return val
		}
		return match // Keep original if no value found
	})
}
