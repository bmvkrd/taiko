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
}

type HTTPTargetConfig struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Timeout string            `yaml:"timeout"`
	Body    string            `yaml:"body"`
	Headers map[string]string `yaml:"headers"`
	RPS     int               `yaml:"rps"`
}

type GRPCTargetConfig struct {
	Endpoint   string            `yaml:"endpoint"`
	Service    string            `yaml:"service"`
	Method     string            `yaml:"method"`
	Timeout    string            `yaml:"timeout"`
	Metadata   map[string]string `yaml:"metadata"`
	Payload    string            `yaml:"payload"`
	RPS        int               `yaml:"rps"`
	ProtoFiles []string          `yaml:"proto_files"`
}

type SchemaRegistryConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SchemaConfig struct {
	Subject string `yaml:"subject"`
	Version int    `yaml:"version"` // 0 means latest
	File    string `yaml:"file"`
}

type KafkaTargetConfig struct {
	Brokers        []string              `yaml:"brokers"`
	Topic          string                `yaml:"topic"`
	Key            string                `yaml:"key"`
	Value          string                `yaml:"value"`
	Headers        map[string]string     `yaml:"headers"`
	RPS            int                   `yaml:"rps"`
	SchemaRegistry *SchemaRegistryConfig `yaml:"schema_registry"`
	KeySchema      *SchemaConfig         `yaml:"key_schema"`
	ValueSchema    *SchemaConfig         `yaml:"value_schema"`
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
	for _, f := range g.ProtoFiles {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("proto_files: cannot access %q: %w (check path and indentation in config)", f, err)
		}
	}
	return nil
}

func (g *GRPCTargetConfig) GetRPS() int {
	return g.RPS
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
	if err := validateSchemaConfig("key_schema", k.KeySchema, k.SchemaRegistry); err != nil {
		return err
	}
	if err := validateSchemaConfig("value_schema", k.ValueSchema, k.SchemaRegistry); err != nil {
		return err
	}
	return nil
}

func validateSchemaConfig(field string, sc *SchemaConfig, sr *SchemaRegistryConfig) error {
	if sc == nil {
		return nil
	}
	if sc.Subject != "" && sc.File != "" {
		return fmt.Errorf("%s: 'subject' and 'file' are mutually exclusive", field)
	}
	if sc.Subject == "" && sc.File == "" {
		return fmt.Errorf("%s: must specify either 'subject' or 'file'", field)
	}
	if sc.Subject != "" {
		if sr == nil || sr.URL == "" {
			return fmt.Errorf("%s: 'schema_registry.url' is required when using 'subject'", field)
		}
	}
	if sc.File != "" {
		if _, err := os.Stat(sc.File); err != nil {
			return fmt.Errorf("%s: cannot access schema file %q: %w", field, sc.File, err)
		}
	}
	return nil
}

func (k *KafkaTargetConfig) GetRPS() int {
	return k.RPS
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
	if files, ok := raw["proto_files"].([]interface{}); ok {
		for _, f := range files {
			if fs, ok := f.(string); ok {
				cfg.ProtoFiles = append(cfg.ProtoFiles, fs)
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

	cfg.SchemaRegistry = parseSchemaRegistryConfig(raw)
	cfg.KeySchema = parseSchemaConfig(raw, "key_schema")
	cfg.ValueSchema = parseSchemaConfig(raw, "value_schema")

	return cfg, nil
}

func parseSchemaRegistryConfig(raw map[string]interface{}) *SchemaRegistryConfig {
	sr := extractMap(raw, "schema_registry")
	if sr == nil {
		return nil
	}
	cfg := &SchemaRegistryConfig{}
	if v, ok := sr["url"].(string); ok {
		cfg.URL = v
	}
	if v, ok := sr["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := sr["password"].(string); ok {
		cfg.Password = v
	}
	return cfg
}

func parseSchemaConfig(raw map[string]interface{}, key string) *SchemaConfig {
	sc := extractMap(raw, key)
	if sc == nil {
		return nil
	}
	cfg := &SchemaConfig{}
	if v, ok := sc["subject"].(string); ok {
		cfg.Subject = v
	}
	if v, ok := sc["version"].(int); ok {
		cfg.Version = v
	}
	if v, ok := sc["file"].(string); ok {
		cfg.File = v
	}
	return cfg
}

func extractMap(raw map[string]interface{}, key string) map[string]interface{} {
	if m, ok := raw[key].(map[interface{}]interface{}); ok {
		result := make(map[string]interface{}, len(m))
		for k, v := range m {
			if ks, ok := k.(string); ok {
				result[ks] = v
			}
		}
		return result
	}
	if m, ok := raw[key].(map[string]interface{}); ok {
		return m
	}
	return nil
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
