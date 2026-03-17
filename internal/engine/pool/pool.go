package pool

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/metrics"
)

// WorkerFunc is the protocol-specific function that performs one unit of work.
// The pool calls this with the index of the selected target (after rate limiting).
// The engine uses this index to look up its own typed target and execute the request.
type WorkerFunc func(ctx context.Context, targetIndex int) *engine.Result

// Config holds the configuration for creating a new Pool.
type Config struct {
	Duration         time.Duration
	Targets          []TargetMeta
	MetricsConnector metrics.Connector
	WorkerFunc       WorkerFunc
	ResultBufferSize int       // defaults to 10000 if zero
	SampleSize       int       // defaults to 10000 if zero
	Logger           io.Writer // defaults to os.Stdout if nil
}

// Pool manages a dynamic worker pool, result collection, auto-scaling, and stats.
type Pool struct {
	// Configuration (immutable after construction)
	duration time.Duration
	totalRPS int
	targets  []TargetMeta
	doWork   WorkerFunc

	// Worker management
	activeWorkers    int32        // atomic
	requestCounter   int64        // atomic
	lastRequestCount atomic.Int64 // atomic — read/written by scaler goroutine
	workerControl    chan bool
	workerWg         sync.WaitGroup

	// Result pipeline
	results          chan *engine.Result
	metricsConnector metrics.Connector
	logger           io.Writer

	// Statistics
	stats          *engine.Stats
	mu             sync.Mutex
	latencySamples []time.Duration
	sampleSize     int
	sampleMu       sync.Mutex
}

// New creates a Pool from the given Config.
func New(cfg Config) (*Pool, error) {
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("pool requires at least one target")
	}
	if cfg.Duration <= 0 {
		return nil, fmt.Errorf("pool requires duration > 0")
	}
	if cfg.WorkerFunc == nil {
		return nil, fmt.Errorf("pool requires a WorkerFunc")
	}

	var totalRPS int
	for i, t := range cfg.Targets {
		if t.RPS <= 0 {
			return nil, fmt.Errorf("target[%d]: RPS must be > 0", i)
		}
		totalRPS += t.RPS
	}

	resultBufSize := cfg.ResultBufferSize
	if resultBufSize <= 0 {
		resultBufSize = 10000
	}
	sampleSize := cfg.SampleSize
	if sampleSize <= 0 {
		sampleSize = 10000
	}

	logger := cfg.Logger
	if logger == nil {
		logger = os.Stdout
	}

	return &Pool{
		duration:         cfg.Duration,
		totalRPS:         totalRPS,
		targets:          cfg.Targets,
		doWork:           cfg.WorkerFunc,
		metricsConnector: cfg.MetricsConnector,
		logger:           logger,
		results:          make(chan *engine.Result, resultBufSize),
		workerControl:    make(chan bool, 1000),
		sampleSize:       sampleSize,
		latencySamples:   make([]time.Duration, 0, sampleSize),
		stats: &engine.Stats{
			StatusCodes: make(map[int]int64),
			Errors:      make(map[string]int64),
			MinLatency:  time.Duration(1<<63 - 1),
		},
	}, nil
}

// Run executes the load test with dynamic scaling. Blocks until completion.
func (p *Pool) Run(ctx context.Context) (*engine.Stats, error) {
	if p.totalRPS <= 0 {
		return nil, fmt.Errorf("total RPS must be > 0")
	}

	testCtx, cancel := context.WithTimeout(ctx, p.duration)
	defer cancel()

	// Start result collector — use parent ctx (not testCtx) so that
	// OnComplete can still run after the test duration expires.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.collectResults(ctx)
	}()

	// Start dynamic scaler
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.dynamicScaler(testCtx)
	}()

	// Start with initial workers: match target RPS for low values, cap at 10 otherwise
	initialWorkers := min(p.totalRPS, 10)
	for i := 0; i < initialWorkers; i++ {
		atomic.AddInt32(&p.activeWorkers, 1)
		p.workerWg.Add(1)
		go func(workerID int) {
			defer p.workerWg.Done()
			defer atomic.AddInt32(&p.activeWorkers, -1)
			p.worker(testCtx, workerID)
		}(i)
	}

	// Wait for test to complete
	<-testCtx.Done()

	// Signal all workers to stop
	close(p.workerControl)

	// Wait for all workers (initial + dynamically added) to finish
	p.workerWg.Wait()
	close(p.results)

	// Wait for collectors
	wg.Wait()

	return p.stats, nil
}

// worker is a goroutine that generates load.
func (p *Pool) worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case shouldStop, ok := <-p.workerControl:
			if !ok || !shouldStop {
				return // Stop signal or channel closed
			}
		default:
			// Select target weighted by RPS
			targetIndex := p.selectTarget()

			// Wait for this target's rate limiter token
			if err := p.targets[targetIndex].Limiter.Wait(ctx); err != nil {
				return
			}

			// Delegate to protocol-specific work function
			result := p.doWork(ctx, targetIndex)

			// Discard results from requests interrupted by context cancellation
			if ctx.Err() != nil {
				return
			}

			atomic.AddInt64(&p.requestCounter, 1)

			// Send result to collector
			select {
			case p.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Close releases pool resources.
func (p *Pool) Close() error {
	if p.metricsConnector != nil {
		return p.metricsConnector.Close()
	}
	return nil
}
