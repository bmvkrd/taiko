package grpc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	protocompile "github.com/bufbuild/protocompile"
	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/engine/pool"
	"github.com/bmvkrd/taiko/internal/metrics"
	grpcreflectv2 "github.com/jhump/protoreflect/v2/grpcreflect"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcmd "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
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
	inputType  protoreflect.MessageDescriptor
	outputType protoreflect.MessageDescriptor
}

// GRPCEngine implements the gRPC load testing protocol.
type GRPCEngine struct {
	pool        *pool.Pool
	targets     []*grpcTarget
	substitutor *engine.Substitutor
}

// NewGRPCEngine creates a new gRPC load generation engine.
// It connects to each target and resolves request/response message types either
// from user-provided .proto files or via server reflection.
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

		var inputType, outputType protoreflect.MessageDescriptor
		var reflErr error
		if len(grpcCfg.ProtoFiles) > 0 {
			fmt.Printf("target[%d]: resolving method types from proto files: %v\n", i, grpcCfg.ProtoFiles)
			inputType, outputType, reflErr = resolveMethodTypesFromProto(grpcCfg.Service, grpcCfg.Method, grpcCfg.ProtoFiles)
		} else {
			fmt.Printf("target[%d]: resolving method types via server reflection\n", i)
			inputType, outputType, reflErr = resolveMethodTypes(conn, grpcCfg.Service, grpcCfg.Method)
		}
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
	inputMsg := dynamicpb.NewMessage(target.inputType)
	if payloadJSON != "" {
		if err := protojson.Unmarshal([]byte(payloadJSON), inputMsg); err != nil {
			result.Error = fmt.Errorf("invalid payload JSON: %w", err)
			result.Duration = time.Since(start)
			return result
		}
	}

	outputMsg := dynamicpb.NewMessage(target.outputType)

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

// resolveMethodTypesFromProto parses the given .proto files and returns the
// input and output message descriptors for the given service/method pair.
// Import paths are derived automatically from the directories of the proto files.
func resolveMethodTypesFromProto(service, method string, protoFiles []string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, error) {
	// Derive import paths from the directories of the proto files.
	seen := map[string]bool{}
	var importPaths []string
	for _, f := range protoFiles {
		dir := filepath.Dir(f)
		if !seen[dir] {
			seen[dir] = true
			importPaths = append(importPaths, dir)
		}
	}

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
			// Open absolute paths directly, then fall back to searching import dirs.
			if f, err := os.Open(path); err == nil {
				return protocompile.SearchResult{Source: f}, nil
			}
			for _, dir := range importPaths {
				if f, err := os.Open(filepath.Join(dir, path)); err == nil {
					return protocompile.SearchResult{Source: f}, nil
				}
			}
			return protocompile.SearchResult{}, protoregistry.NotFound
		})),
	}

	fds, err := compiler.Compile(context.Background(), protoFiles...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile proto files: %w", err)
	}

	// DFS across file descriptors and their transitive imports to find the service.
	var found protoreflect.MethodDescriptor
	var serviceFound bool
	visited := map[string]bool{}
	var search func(fd protoreflect.FileDescriptor)
	search = func(fd protoreflect.FileDescriptor) {
		if visited[fd.Path()] {
			return
		}
		visited[fd.Path()] = true
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			if string(svc.FullName()) == service {
				serviceFound = true
				found = svc.Methods().ByName(protoreflect.Name(method))
				return
			}
		}
		if serviceFound {
			return
		}
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			search(imports.Get(i))
			if serviceFound {
				return
			}
		}
	}
	for _, fd := range fds {
		search(fd)
		if serviceFound {
			break
		}
	}

	if !serviceFound {
		return nil, nil, fmt.Errorf("service %q not found in provided proto files", service)
	}
	if found == nil {
		return nil, nil, fmt.Errorf("method %q not found in service %q", method, service)
	}
	return found.Input(), found.Output(), nil
}

// resolveMethodTypes uses gRPC server reflection to get the input and output
// message descriptors for the given service/method pair.
func resolveMethodTypes(conn *grpc.ClientConn, service, method string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc := grpcreflectv2.NewClientAuto(ctx, conn)
	defer rc.Reset()

	fd, err := rc.FileContainingSymbol(protoreflect.FullName(service))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve service %q via reflection: %w", service, err)
	}

	var svcDesc protoreflect.ServiceDescriptor
	svcs := fd.Services()
	for i := 0; i < svcs.Len(); i++ {
		svc := svcs.Get(i)
		if string(svc.FullName()) == service {
			svcDesc = svc
			break
		}
	}
	if svcDesc == nil {
		return nil, nil, fmt.Errorf("service %q not found in reflection response", service)
	}

	mthDesc := svcDesc.Methods().ByName(protoreflect.Name(method))
	if mthDesc == nil {
		return nil, nil, fmt.Errorf("method %q not found in service %q", method, service)
	}

	return mthDesc.Input(), mthDesc.Output(), nil
}

// closeConnections closes all gRPC connections in the given slice.
func closeConnections(targets []*grpcTarget) {
	for _, t := range targets {
		if t.conn != nil {
			t.conn.Close()
		}
	}
}
