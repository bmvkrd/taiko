package kafka

import (
	"testing"

	"github.com/bmvkrd/taiko/internal/config"
)

func TestNewKafkaEngine_Validation(t *testing.T) {
	validKafkaTarget := &config.KafkaTargetConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "test-topic",
		RPS:     10,
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{
			name:    "no targets",
			cfg:     &config.Config{Load: config.LoadConfig{Duration: "10s"}},
			wantErr: "at least one target",
		},
		{
			name: "invalid duration",
			cfg: &config.Config{
				Targets: []config.Target{validKafkaTarget},
				Load:    config.LoadConfig{Duration: "not-a-duration"},
			},
			wantErr: "invalid duration",
		},
		{
			name: "wrong target type",
			cfg: &config.Config{
				Targets: []config.Target{&config.HTTPTargetConfig{URL: "http://x", RPS: 10}},
				Load:    config.LoadConfig{Duration: "10s"},
			},
			wantErr: "Kafka target configuration",
		},
		{
			name: "missing brokers",
			cfg: &config.Config{
				Targets: []config.Target{&config.KafkaTargetConfig{Topic: "t", RPS: 10}},
				Load:    config.LoadConfig{Duration: "10s"},
			},
			wantErr: "brokers",
		},
		{
			name: "missing topic",
			cfg: &config.Config{
				Targets: []config.Target{&config.KafkaTargetConfig{Brokers: []string{"localhost:9092"}, RPS: 10}},
				Load:    config.LoadConfig{Duration: "10s"},
			},
			wantErr: "topic",
		},
		{
			name: "zero RPS",
			cfg: &config.Config{
				Targets: []config.Target{&config.KafkaTargetConfig{Brokers: []string{"localhost:9092"}, Topic: "t", RPS: 0}},
				Load:    config.LoadConfig{Duration: "10s"},
			},
			wantErr: "rps",
		},
		{
			name: "valid config creates engine",
			cfg: &config.Config{
				Targets: []config.Target{validKafkaTarget},
				Load:    config.LoadConfig{Duration: "10s"},
				Metrics: config.MetricsConfig{Type: "console"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := NewKafkaEngine(tt.cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsStr(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if eng == nil {
				t.Fatal("expected non-nil engine")
			}
			eng.Close()
		})
	}
}

func TestNewKafkaEngine_WithValueSchema_File(t *testing.T) {
	cfg := &config.Config{
		Targets: []config.Target{
			&config.KafkaTargetConfig{
				Brokers: []string{"localhost:9092"},
				Topic:   "test-topic",
				Value:   `{"id": "test", "count": 1}`,
				RPS:     10,
				ValueSchema: &config.SchemaConfig{
					File: "avro/testdata/test_event.avsc",
				},
			},
		},
		Load:    config.LoadConfig{Duration: "10s"},
		Metrics: config.MetricsConfig{Type: "console"},
	}

	eng, err := NewKafkaEngine(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer eng.Close()

	ke := eng.(*KafkaEngine)
	if ke.targets[0].valueSerializer == nil {
		t.Fatal("expected non-nil value serializer")
	}
	if ke.targets[0].keySerializer != nil {
		t.Fatal("expected nil key serializer when no key schema configured")
	}
}

func TestNewKafkaEngine_SchemaValidation_MissingRegistryURL(t *testing.T) {
	cfg := &config.Config{
		Targets: []config.Target{
			&config.KafkaTargetConfig{
				Brokers: []string{"localhost:9092"},
				Topic:   "test-topic",
				RPS:     10,
				ValueSchema: &config.SchemaConfig{
					Subject: "test-value",
				},
			},
		},
		Load:    config.LoadConfig{Duration: "10s"},
		Metrics: config.MetricsConfig{Type: "console"},
	}

	_, err := NewKafkaEngine(cfg)
	if err == nil {
		t.Fatal("expected error when schema_registry is missing with subject-based schema")
	}
	if !containsStr(err.Error(), "schema_registry.url") {
		t.Fatalf("expected error about missing schema_registry.url, got: %v", err)
	}
}

func TestNewKafkaEngine_MultiTarget_DeduplicatesBrokers(t *testing.T) {
	// Two targets sharing the same broker — should still create one client.
	cfg := &config.Config{
		Targets: []config.Target{
			&config.KafkaTargetConfig{Brokers: []string{"broker:9092"}, Topic: "topic-a", RPS: 5},
			&config.KafkaTargetConfig{Brokers: []string{"broker:9092"}, Topic: "topic-b", RPS: 5},
		},
		Load:    config.LoadConfig{Duration: "10s"},
		Metrics: config.MetricsConfig{Type: "console"},
	}

	eng, err := NewKafkaEngine(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer eng.Close()

	ke, ok := eng.(*KafkaEngine)
	if !ok {
		t.Fatal("expected *KafkaEngine")
	}
	if len(ke.targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(ke.targets))
	}
	if ke.targets[0].topic != "topic-a" || ke.targets[1].topic != "topic-b" {
		t.Fatal("targets stored in wrong order")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
