package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/engine/pool"
	"github.com/bmvkrd/taiko/internal/metrics"
	"golang.org/x/time/rate"
)

func init() {
	engine.Register("http", NewHTTPEngine)
}

// httpTarget holds HTTP-specific per-target state.
type httpTarget struct {
	url     string
	method  string
	body    string
	headers map[string]string
}

// HTTPEngine implements the HTTP load testing protocol.
type HTTPEngine struct {
	pool        *pool.Pool
	targets     []*httpTarget
	substitutor *engine.Substitutor
	client      *http.Client
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

	// Build HTTP targets and pool target metadata
	var httpTargets []*httpTarget
	var poolTargets []pool.TargetMeta
	var maxTimeout time.Duration = 30 * time.Second

	for i, t := range cfg.Targets {
		httpCfg, ok := t.(*config.HTTPTargetConfig)
		if !ok {
			return nil, fmt.Errorf("target[%d]: http engine requires HTTP target configuration", i)
		}

		if httpCfg.URL == "" {
			return nil, fmt.Errorf("target[%d]: http engine requires 'url' config", i)
		}

		if httpCfg.RPS <= 0 {
			return nil, fmt.Errorf("target[%d]: http engine requires 'rps' > 0", i)
		}

		method := httpCfg.Method
		if method == "" {
			method = "GET"
		}

		// Parse request timeout from target config
		timeout := 30 * time.Second
		if httpCfg.Timeout != "" {
			if t, err := time.ParseDuration(httpCfg.Timeout); err == nil {
				timeout = t
			}
		}
		if timeout > maxTimeout {
			maxTimeout = timeout
		}

		httpTargets = append(httpTargets, &httpTarget{
			url:     httpCfg.URL,
			method:  method,
			body:    httpCfg.Body,
			headers: httpCfg.Headers,
		})

		poolTargets = append(poolTargets, pool.TargetMeta{
			RPS:     httpCfg.RPS,
			Limiter: rate.NewLimiter(rate.Limit(httpCfg.RPS), httpCfg.GetBurst()),
		})
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

	// Initialize variable substitutor
	substitutor, err := engine.NewSubstitutor(cfg.Variables)
	if err != nil {
		return nil, err
	}

	eng := &HTTPEngine{
		targets:     httpTargets,
		substitutor: substitutor,
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
	}

	// Create the pool, passing eng.doWork as the WorkerFunc
	p, err := pool.New(pool.Config{
		Duration:         duration,
		Targets:          poolTargets,
		MetricsConnector: metricsConnector,
		WorkerFunc:       eng.doWork,
	})
	if err != nil {
		return nil, fmt.Errorf("pool creation error: %w", err)
	}
	eng.pool = p

	return eng, nil
}

// Run executes the load test with dynamic scaling
func (e *HTTPEngine) Run(ctx context.Context) (*engine.Stats, error) {
	fmt.Println("Starting load test...")
	fmt.Println("")

	stats, err := e.pool.Run(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Println("\nLoad test completed!")
	return stats, nil
}

// Close releases engine resources
func (e *HTTPEngine) Close() error {
	return e.pool.Close()
}

// doWork performs a single HTTP request to the target at the given index.
func (e *HTTPEngine) doWork(_ context.Context, targetIndex int) *engine.Result {
	target := e.targets[targetIndex]

	// Generate values once for this request and apply to all templates
	values := e.substitutor.NewValues()
	url := e.substitutor.Apply(target.url, values)
	body := e.substitutor.Apply(target.body, values)

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

