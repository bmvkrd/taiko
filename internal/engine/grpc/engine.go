package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/engine/pool"
	"github.com/bmvkrd/taiko/internal/metrics"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcmd "google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
)

func init() {
	engine.Register("grpc", NewGRPCEngine)
}

// grpcTarget holds gRPC-specific per-target state.
type grpcTarget struct {
	fullMethod string
	payload    string
	metadata   map[string]string
	timeout    time.Duration
	conn       *grpc.ClientConn
	inputType  *desc.MessageDescriptor
	outputType *desc.MessageDescriptor
}

// GRPCEngine implements the gRPC load testing protocol.
type GRPCEngine struct {
	pool        *pool.Pool
	targets     []*grpcTarget
	substitutor *engine.Substitutor
}

// NewGRPCEngine creates a new gRPC load generation engine.
// It connects to each target and uses server reflection to discover
// the request/response message types for the configured service method.
func NewGRPCEngine(cfg *config.Config) (engine.Engine, error) {
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("grpc engine requires at least one target")
	}

	duration, err := time.ParseDuration(cfg.Load.Duration)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	var grpcTargets []*grpcTarget
	var poolTargets []pool.TargetMeta

	for i, t := range cfg.Targets {
		grpcCfg, ok := t.(*config.GRPCTargetConfig)
		if !ok {
			return nil, fmt.Errorf("target[%d]: grpc engine requires gRPC target configuration", i)
		}

		timeout := 30 * time.Second
		if grpcCfg.Timeout != "" {
			if parsed, parseErr := time.ParseDuration(grpcCfg.Timeout); parseErr == nil {
				timeout = parsed
			}
		}

		conn, connErr := grpc.NewClient(
			grpcCfg.Endpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if connErr != nil {
			closeConnections(grpcTargets)
			return nil, fmt.Errorf("target[%d]: failed to create connection to %s: %w", i, grpcCfg.Endpoint, connErr)
		}

		inputType, outputType, reflErr := resolveMethodTypes(conn, grpcCfg.Service, grpcCfg.Method)
		if reflErr != nil {
			conn.Close()
			closeConnections(grpcTargets)
			return nil, fmt.Errorf("target[%d]: %w", i, reflErr)
		}

		grpcTargets = append(grpcTargets, &grpcTarget{
			fullMethod: fmt.Sprintf("/%s/%s", grpcCfg.Service, grpcCfg.Method),
			payload:    grpcCfg.Payload,
			metadata:   grpcCfg.Metadata,
			timeout:    timeout,
			conn:       conn,
			inputType:  inputType,
			outputType: outputType,
		})

		poolTargets = append(poolTargets, pool.TargetMeta{
			RPS:     grpcCfg.RPS,
			Limiter: rate.NewLimiter(rate.Limit(grpcCfg.RPS), grpcCfg.GetBurst()),
		})
	}

	metricsType := cfg.Metrics.Type
	if metricsType == "" {
		metricsType = "console"
	}
	metricsConnector, err := metrics.Get(metricsType)
	if err != nil {
		closeConnections(grpcTargets)
		return nil, fmt.Errorf("metrics connector error: %w", err)
	}
	if err := metricsConnector.Init(cfg.Metrics.Config); err != nil {
		closeConnections(grpcTargets)
		return nil, fmt.Errorf("metrics connector init error: %w", err)
	}

	substitutor, err := engine.NewSubstitutor(cfg.Variables)
	if err != nil {
		closeConnections(grpcTargets)
		return nil, err
	}

	eng := &GRPCEngine{
		targets:     grpcTargets,
		substitutor: substitutor,
	}

	p, err := pool.New(pool.Config{
		Duration:         duration,
		Targets:          poolTargets,
		MetricsConnector: metricsConnector,
		WorkerFunc:       eng.doWork,
	})
	if err != nil {
		closeConnections(grpcTargets)
		return nil, fmt.Errorf("pool creation error: %w", err)
	}
	eng.pool = p

	return eng, nil
}

// Run executes the gRPC load test with dynamic scaling.
func (e *GRPCEngine) Run(ctx context.Context) (*engine.Stats, error) {
	fmt.Println("Starting gRPC load test...")
	fmt.Println("")

	stats, err := e.pool.Run(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Println("\ngRPC load test completed!")
	return stats, nil
}

// Close releases engine resources.
func (e *GRPCEngine) Close() error {
	poolErr := e.pool.Close()
	closeConnections(e.targets)
	return poolErr
}

// doWork performs a single gRPC unary call to the target at the given index.
func (e *GRPCEngine) doWork(ctx context.Context, targetIndex int) *engine.Result {
	target := e.targets[targetIndex]

	// Generate substitution values once; apply to payload and all metadata values.
	values := e.substitutor.NewValues()
	payloadJSON := e.substitutor.Apply(target.payload, values)

	result := &engine.Result{
		Timestamp: time.Now(),
		TargetURL: target.fullMethod,
	}

	start := time.Now()

	// Build input message from the (substituted) JSON payload.
	inputMsg := dynamic.NewMessage(target.inputType)
	if payloadJSON != "" {
		if err := inputMsg.UnmarshalJSON([]byte(payloadJSON)); err != nil {
			result.Error = fmt.Errorf("invalid payload JSON: %w", err)
			result.Duration = time.Since(start)
			return result
		}
	}

	outputMsg := dynamic.NewMessage(target.outputType)

	// Derive a per-request context from the worker context so that
	// in-flight requests are bounded by both the request timeout and
	// the overall test lifetime.
	reqCtx, cancel := context.WithTimeout(ctx, target.timeout)
	defer cancel()

	if len(target.metadata) > 0 {
		md := grpcmd.New(nil)
		for k, v := range target.metadata {
			md.Append(k, e.substitutor.Apply(v, values))
		}
		reqCtx = grpcmd.NewOutgoingContext(reqCtx, md)
	}

	err := target.conn.Invoke(reqCtx, target.fullMethod, inputMsg, outputMsg)
	result.Duration = time.Since(start)

	if err != nil {
		st, _ := status.FromError(err)
		result.StatusCode = int(st.Code())
		result.Error = err
		result.Success = false
		return result
	}

	result.StatusCode = int(codes.OK)
	result.Success = true
	return result
}

// resolveMethodTypes uses gRPC server reflection to get the input and output
// message descriptors for the given service/method pair.
func resolveMethodTypes(conn *grpc.ClientConn, service, method string) (*desc.MessageDescriptor, *desc.MessageDescriptor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc := grpcreflect.NewClient(ctx, reflectpb.NewServerReflectionClient(conn))
	defer rc.Reset()

	svcDesc, err := rc.ResolveService(service)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve service %q via reflection: %w", service, err)
	}

	mthDesc := svcDesc.FindMethodByName(method)
	if mthDesc == nil {
		return nil, nil, fmt.Errorf("method %q not found in service %q", method, service)
	}

	return mthDesc.GetInputType(), mthDesc.GetOutputType(), nil
}

// closeConnections closes all gRPC connections in the given slice.
func closeConnections(targets []*grpcTarget) {
	for _, t := range targets {
		if t.conn != nil {
			t.conn.Close()
		}
	}
}
