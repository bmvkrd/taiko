package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	"github.com/bmvkrd/taiko/internal/engine/pool"
	"github.com/bmvkrd/taiko/internal/metrics"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/time/rate"
)

func init() {
	engine.Register("kafka", NewKafkaEngine)
}

// kafkaTarget holds Kafka-specific per-target state.
type kafkaTarget struct {
	topic   string
	key     string
	value   string
	headers map[string]string
}

// KafkaEngine implements the Kafka load testing protocol.
type KafkaEngine struct {
	pool        *pool.Pool
	targets     []*kafkaTarget
	substitutor *engine.Substitutor
	client      *kgo.Client
}

// NewKafkaEngine creates a new Kafka load generation engine.
func NewKafkaEngine(cfg *config.Config) (engine.Engine, error) {
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("kafka engine requires at least one target")
	}

	duration, err := time.ParseDuration(cfg.Load.Duration)
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	// Build Kafka targets and pool target metadata. Collect all brokers across
	// targets so a single client can serve all of them.
	var kafkaTargets []*kafkaTarget
	var poolTargets []pool.TargetMeta
	brokerSet := make(map[string]struct{})
	var allBrokers []string

	for i, t := range cfg.Targets {
		kafkaCfg, ok := t.(*config.KafkaTargetConfig)
		if !ok {
			return nil, fmt.Errorf("target[%d]: kafka engine requires Kafka target configuration", i)
		}
		if len(kafkaCfg.Brokers) == 0 {
			return nil, fmt.Errorf("target[%d]: kafka engine requires 'brokers' config", i)
		}
		if kafkaCfg.Topic == "" {
			return nil, fmt.Errorf("target[%d]: kafka engine requires 'topic' config", i)
		}
		if kafkaCfg.RPS <= 0 {
			return nil, fmt.Errorf("target[%d]: kafka engine requires 'rps' > 0", i)
		}

		kafkaTargets = append(kafkaTargets, &kafkaTarget{
			topic:   kafkaCfg.Topic,
			key:     kafkaCfg.Key,
			value:   kafkaCfg.Value,
			headers: kafkaCfg.Headers,
		})

		poolTargets = append(poolTargets, pool.TargetMeta{
			RPS:     kafkaCfg.RPS,
			Limiter: rate.NewLimiter(rate.Limit(kafkaCfg.RPS), kafkaCfg.GetBurst()),
		})

		for _, b := range kafkaCfg.Brokers {
			if _, seen := brokerSet[b]; !seen {
				brokerSet[b] = struct{}{}
				allBrokers = append(allBrokers, b)
			}
		}
	}

	// Initialize metrics connector from config.
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

	// Initialize variable substitutor.
	substitutor, err := engine.NewSubstitutor(cfg.Variables)
	if err != nil {
		return nil, err
	}

	// Create a single franz-go client shared across all workers. Connections are
	// established lazily on first produce, so this never fails due to broker
	// availability. Idempotent writes are disabled for lower overhead.
	client, err := kgo.NewClient(
		kgo.SeedBrokers(allBrokers...),
		kgo.RequiredAcks(kgo.LeaderAck()),
		kgo.DisableIdempotentWrite(),
		kgo.ProducerBatchCompression(kgo.NoCompression()),
	)
	if err != nil {
		metricsConnector.Close()
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	eng := &KafkaEngine{
		targets:     kafkaTargets,
		substitutor: substitutor,
		client:      client,
	}

	p, err := pool.New(pool.Config{
		Duration:         duration,
		Targets:          poolTargets,
		MetricsConnector: metricsConnector,
		WorkerFunc:       eng.doWork,
	})
	if err != nil {
		client.Close()
		metricsConnector.Close()
		return nil, fmt.Errorf("pool creation error: %w", err)
	}
	eng.pool = p

	return eng, nil
}

// Run executes the load test with dynamic scaling.
func (e *KafkaEngine) Run(ctx context.Context) (*engine.Stats, error) {
	fmt.Println("Starting load test...")
	fmt.Println("")

	stats, err := e.pool.Run(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Println("\nLoad test completed!")
	return stats, nil
}

// Close releases engine resources.
func (e *KafkaEngine) Close() error {
	poolErr := e.pool.Close()
	e.client.Close()
	return poolErr
}

// doWork produces a single Kafka record to the target at the given index.
func (e *KafkaEngine) doWork(ctx context.Context, targetIndex int) *engine.Result {
	target := e.targets[targetIndex]

	// Generate values once per record and apply to all templates.
	values := e.substitutor.NewValues()
	key := e.substitutor.Apply(target.key, values)
	value := e.substitutor.Apply(target.value, values)

	result := &engine.Result{
		Timestamp: time.Now(),
		TargetURL: target.topic,
	}

	record := &kgo.Record{
		Topic: target.topic,
		Value: []byte(value),
	}
	if key != "" {
		record.Key = []byte(key)
	}
	for k, v := range target.headers {
		record.Headers = append(record.Headers, kgo.RecordHeader{
			Key:   k,
			Value: []byte(e.substitutor.Apply(v, values)),
		})
	}

	start := time.Now()
	results := e.client.ProduceSync(ctx, record)
	result.Duration = time.Since(start)

	if err := results.FirstErr(); err != nil {
		result.Error = err
		return result
	}

	result.Success = true
	return result
}
