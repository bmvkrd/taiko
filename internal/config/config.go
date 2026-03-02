package config

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v2"
)

type Variable struct {
	Name      string         `yaml:"name"`
	Type      string         `yaml:"type"`
	Generator map[string]any `yaml:"generator"`
}

type YAMLConfig struct {
	Engine    string                   `yaml:"engine"`
	Variables []Variable               `yaml:"variables,omitempty"`
	Targets   []map[string]interface{} `yaml:"targets"`
	Load      LoadConfig               `yaml:"load"`
	Metrics   MetricsConfig            `yaml:"metrics"`
}

type Target interface {
	ToEngineConfig() map[string]string
	Validate() error
	GetRPS() int
	GetBurst() int
}

type HTTPTargetConfig struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Timeout string            `yaml:"timeout"`
	Body    string            `yaml:"body"`
	Headers map[string]string `yaml:"headers"`
	RPS     int               `yaml:"rps"`
	Burst   int               `yaml:"burst"`
}

type GRPCTargetConfig struct {
	Endpoint string            `yaml:"endpoint"`
	Service  string            `yaml:"service"`
	Method   string            `yaml:"method"`
	Timeout  string            `yaml:"timeout"`
	Metadata map[string]string `yaml:"metadata"`
	Payload  string            `yaml:"payload"`
	RPS      int               `yaml:"rps"`
	Burst    int               `yaml:"burst"`
}

type KafkaTargetConfig struct {
	Brokers []string          `yaml:"brokers"`
	Topic   string            `yaml:"topic"`
	Key     string            `yaml:"key"`
	Value   string            `yaml:"value"`
	Headers map[string]string `yaml:"headers"`
	RPS     int               `yaml:"rps"`
	Burst   int               `yaml:"burst"`
}

type ParamType string

const (
    StringType ParamType = "string"
    IntType    ParamType = "int"
    FloatType  ParamType = "float"
    BoolType   ParamType = "bool"
)

func (h *HTTPTargetConfig) ToEngineConfig() map[string]string {
	cfg := map[string]string{
		"url":     h.URL,
		"method":  h.Method,
		"body":    h.Body,
		"timeout": h.Timeout,
	}
	for k, v := range h.Headers {
		cfg["header."+k] = v
	}
	return cfg
}

func (h *HTTPTargetConfig) Validate() error {
	if h.URL == "" {
		return fmt.Errorf("http target requires 'url' field")
	}
	if h.RPS <= 0 {
		return fmt.Errorf("http target requires 'rps' field > 0")
	}
	return nil
}

func (h *HTTPTargetConfig) GetRPS() int {
	return h.RPS
}

func (h *HTTPTargetConfig) GetBurst() int {
	if h.Burst <= 0 {
		return 1
	}
	return h.Burst
}

func (g *GRPCTargetConfig) ToEngineConfig() map[string]string {
	cfg := map[string]string{
		"endpoint": g.Endpoint,
		"service":  g.Service,
		"method":   g.Method,
		"payload":  g.Payload,
	}
	for k, v := range g.Metadata {
		cfg["metadata."+k] = v
	}
	return cfg
}

func (g *GRPCTargetConfig) Validate() error {
	if g.Endpoint == "" {
		return fmt.Errorf("grpc target requires 'endpoint' field")
	}
	if g.Service == "" {
		return fmt.Errorf("grpc target requires 'service' field")
	}
	if g.Method == "" {
		return fmt.Errorf("grpc target requires 'method' field")
	}
	if g.RPS <= 0 {
		return fmt.Errorf("grpc target requires 'rps' field > 0")
	}
	return nil
}

func (g *GRPCTargetConfig) GetRPS() int {
	return g.RPS
}

func (g *GRPCTargetConfig) GetBurst() int {
	if g.Burst <= 0 {
		return 1
	}
	return g.Burst
}

func (k *KafkaTargetConfig) ToEngineConfig() map[string]string {
	cfg := map[string]string{
		"brokers": strings.Join(k.Brokers, ","),
		"topic":   k.Topic,
		"key":     k.Key,
		"value":   k.Value,
	}
	for key, v := range k.Headers {
		cfg["header."+key] = v
	}
	return cfg
}

func (k *KafkaTargetConfig) Validate() error {
	if len(k.Brokers) == 0 {
		return fmt.Errorf("kafka target requires 'brokers' field")
	}
	if k.Topic == "" {
		return fmt.Errorf("kafka target requires 'topic' field")
	}
	if k.RPS <= 0 {
		return fmt.Errorf("kafka target requires 'rps' field > 0")
	}
	return nil
}

func (k *KafkaTargetConfig) GetRPS() int {
	return k.RPS
}

func (k *KafkaTargetConfig) GetBurst() int {
	if k.Burst <= 0 {
		return 1
	}
	return k.Burst
}

type TargetParser func(raw map[string]interface{}) (Target, error)

var targetParsers = map[string]TargetParser{
	"http":  parseHTTPTarget,
	"grpc":  parseGRPCTarget,
	"kafka": parseKafkaTarget,
}

func ParseTarget(engine string, raw map[string]interface{}) (Target, error) {
	parser, ok := targetParsers[engine]
	if !ok {
		return nil, fmt.Errorf("unknown engine type: %s", engine)
	}
	return parser(raw)
}

func parseHTTPTarget(raw map[string]interface{}) (Target, error) {
	cfg := &HTTPTargetConfig{
		Headers: make(map[string]string),
	}

	if v, ok := raw["url"].(string); ok {
		cfg.URL = v
	}
	if v, ok := raw["method"].(string); ok {
		cfg.Method = v
	}
	if v, ok := raw["timeout"].(string); ok {
		cfg.Timeout = v
	}
	if v, ok := raw["body"].(string); ok {
		cfg.Body = v
	}
	if v, ok := raw["rps"].(int); ok {
		cfg.RPS = v
	}
	if v, ok := raw["burst"].(int); ok {
		cfg.Burst = v
	}
	if headers, ok := raw["headers"].(map[interface{}]interface{}); ok {
		for k, v := range headers {
			if ks, ok := k.(string); ok {
				if vs, ok := v.(string); ok {
					cfg.Headers[ks] = vs
				}
			}
		}
	}
	if headers, ok := raw["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				cfg.Headers[k] = vs
			}
		}
	}

	return cfg, nil
}

func parseGRPCTarget(raw map[string]interface{}) (Target, error) {
	cfg := &GRPCTargetConfig{
		Metadata: make(map[string]string),
	}

	if v, ok := raw["endpoint"].(string); ok {
		cfg.Endpoint = v
	}
	if v, ok := raw["service"].(string); ok {
		cfg.Service = v
	}
	if v, ok := raw["method"].(string); ok {
		cfg.Method = v
	}
	if v, ok := raw["timeout"].(string); ok {
		cfg.Timeout = v
	}
	if v, ok := raw["payload"].(string); ok {
		cfg.Payload = v
	}
	if v, ok := raw["rps"].(int); ok {
		cfg.RPS = v
	}
	if v, ok := raw["burst"].(int); ok {
		cfg.Burst = v
	}
	if metadata, ok := raw["metadata"].(map[interface{}]interface{}); ok {
		for k, v := range metadata {
			if ks, ok := k.(string); ok {
				if vs, ok := v.(string); ok {
					cfg.Metadata[ks] = vs
				}
			}
		}
	}
	if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
		for k, v := range metadata {
			if vs, ok := v.(string); ok {
				cfg.Metadata[k] = vs
			}
		}
	}

	return cfg, nil
}

func parseKafkaTarget(raw map[string]interface{}) (Target, error) {
	cfg := &KafkaTargetConfig{
		Headers: make(map[string]string),
	}

	if v, ok := raw["topic"].(string); ok {
		cfg.Topic = v
	}
	if v, ok := raw["key"].(string); ok {
		cfg.Key = v
	}
	if v, ok := raw["value"].(string); ok {
		cfg.Value = v
	}
	if v, ok := raw["rps"].(int); ok {
		cfg.RPS = v
	}
	if v, ok := raw["burst"].(int); ok {
		cfg.Burst = v
	}

	if brokers, ok := raw["brokers"].([]interface{}); ok {
		for _, b := range brokers {
			if bs, ok := b.(string); ok {
				cfg.Brokers = append(cfg.Brokers, bs)
			}
		}
	}

	if headers, ok := raw["headers"].(map[interface{}]interface{}); ok {
		for k, v := range headers {
			if ks, ok := k.(string); ok {
				if vs, ok := v.(string); ok {
					cfg.Headers[ks] = vs
				}
			}
		}
	}
	if headers, ok := raw["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				cfg.Headers[k] = vs
			}
		}
	}

	return cfg, nil
}

type LoadConfig struct {
	Duration string `yaml:"duration"`
}

type MetricsConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

type Config struct {
	Engine    string        // Engine type (http, grpc, kafka, etc.)
	Targets   []Target      // Parsed target configurations
	Variables []Variable    // Variables with generators for substitution
	Load      LoadConfig    // Load test parameters
	Metrics   MetricsConfig // Metrics connector settings
}

func LoadFromFile(path string) (*YAMLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg YAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// FindConfigFile returns the config file path to use based on priority:
// 1. Explicit flag path
// 2. CONFIG_FILE environment variable
// 3. Default path /etc/taiko/config.yaml (if exists)
// Returns empty string if no config file is found
func FindConfigFile(flagPath string) string {
	// 1. Explicit flag takes precedence
	if flagPath != "" {
		return flagPath
	}

	// 2. Environment variable
	if envPath := os.Getenv("CONFIG_FILE"); envPath != "" {
		return envPath
	}

	// 3. Default path (only if exists)
	defaultPath := "/etc/taiko/config.yaml"
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}

	return ""
}
