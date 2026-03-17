package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bmvkrd/livelog"
	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/engine"
	_ "github.com/bmvkrd/taiko/internal/engine/grpc"
	_ "github.com/bmvkrd/taiko/internal/engine/http"
	_ "github.com/bmvkrd/taiko/internal/engine/kafka"
	"github.com/bmvkrd/taiko/internal/metrics"
)

func main() {
	display := livelog.New(os.Stdout)
	defer display.Flush()
	metrics.SetDisplay(display)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Validation error: %v\n", err)
		os.Exit(1)
	}

	var totalRPS int
	rpsBreakdown := make([]string, 0, len(cfg.Targets))
	for _, target := range cfg.Targets {
		rps := target.GetRPS()
		totalRPS += rps
		rpsBreakdown = append(rpsBreakdown, fmt.Sprintf("%d", rps))
	}

	display.Log("LoadEngine Starting...")
	display.Logf("Engine: %s", cfg.Engine)
	display.Logf("Targets: %d", len(cfg.Targets))
	for i, target := range cfg.Targets {
		display.Logf("  [%d] %s", i+1, targetInfo(cfg.Engine, target))
	}
	display.Logf("Duration: %s", cfg.Load.Duration)
	if len(cfg.Targets) > 1 {
		display.Logf("Target RPS: %d total (%s)", totalRPS, strings.Join(rpsBreakdown, " + "))
	} else {
		display.Logf("Target RPS: %d", totalRPS)
	}
	display.Log("Workers: auto-scaling")
	display.Logf("Metrics: %s", cfg.Metrics.Type)
	display.Log(strings.Repeat("-", 50))

	eng, err := engine.Get(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Engine error: %v\n", err)
		os.Exit(1)
	}
	defer eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		display.Log("\nReceived interrupt signal. Shutting down gracefully...")
		cancel()
	}()

	_, err = eng.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load test error: %v\n", err)
		os.Exit(1)
	}
}

func targetInfo(engineType string, target config.Target) string {
	switch engineType {
	case "http":
		if httpTarget, ok := target.(*config.HTTPTargetConfig); ok {
			s := fmt.Sprintf("%s %s (rps: %d", httpTarget.Method, httpTarget.URL, httpTarget.RPS)
			if len(httpTarget.Headers) > 0 {
				s += fmt.Sprintf(", headers: %d", len(httpTarget.Headers))
			}
			return s + ")"
		}
	case "grpc":
		if grpcTarget, ok := target.(*config.GRPCTargetConfig); ok {
			return fmt.Sprintf("%s/%s @ %s (rps: %d)", grpcTarget.Service, grpcTarget.Method, grpcTarget.Endpoint, grpcTarget.RPS)
		}
	case "kafka":
		if kafkaTarget, ok := target.(*config.KafkaTargetConfig); ok {
			s := fmt.Sprintf("topic=%s brokers=%v", kafkaTarget.Topic, kafkaTarget.Brokers)
			if kafkaTarget.KeySchema != nil {
				s += fmt.Sprintf(" key_schema=%s", schemaLabel(kafkaTarget.KeySchema))
			}
			if kafkaTarget.ValueSchema != nil {
				s += fmt.Sprintf(" value_schema=%s", schemaLabel(kafkaTarget.ValueSchema))
			}
			return s + fmt.Sprintf(" (rps: %d)", kafkaTarget.RPS)
		}
	}
	return fmt.Sprintf("%v (rps: %d)", target.ToEngineConfig(), target.GetRPS())
}

func schemaLabel(sc *config.SchemaConfig) string {
	if sc.Subject != "" {
		return sc.Subject
	}
	return sc.File
}

func loadConfig() (*config.Config, error) {
	cfg := &config.Config{}

	configFilePath := parseConfigFileFlag()
	if err := loadFromYAML(cfg, configFilePath); err != nil {
		return nil, err
	}

	return cfg, nil
}

func parseConfigFileFlag() string {
	for i, arg := range os.Args[1:] {
		if arg == "-config-file" || arg == "--config-file" {
			if i+1 < len(os.Args)-1 {
				return os.Args[i+2]
			}
		}
		if strings.HasPrefix(arg, "-config-file=") {
			return strings.TrimPrefix(arg, "-config-file=")
		}
		if strings.HasPrefix(arg, "--config-file=") {
			return strings.TrimPrefix(arg, "--config-file=")
		}
	}
	return ""
}

func loadFromYAML(cfg *config.Config, flagPath string) error {
	path := config.FindConfigFile(flagPath)
	if path == "" {
		return fmt.Errorf("config file not found (use '-config-file' flag, or create '/etc/taiko/config.yaml')")
	}

	yamlConfig, err := config.LoadFromFile(path)
	if err != nil {
		if flagPath != "" {
			return fmt.Errorf("failed to load config file '%s': %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "Warning: could not load config file '%s': %v\n", path, err)
		return nil
	}

	if yamlConfig.Engine != "" {
		cfg.Engine = yamlConfig.Engine
	}

	if len(yamlConfig.Targets) > 0 {
		cfg.Targets = nil
		for i, rawTarget := range yamlConfig.Targets {
			target, err := config.ParseTarget(cfg.Engine, rawTarget)
			if err != nil {
				return fmt.Errorf("failed to parse target[%d]: %w", i, err)
			}
			if err := target.Validate(); err != nil {
				return fmt.Errorf("invalid target[%d]: %w", i, err)
			}
			cfg.Targets = append(cfg.Targets, target)
		}
	}

	if yamlConfig.Load.Duration != "" {
		cfg.Load.Duration = yamlConfig.Load.Duration
	}

	if yamlConfig.Metrics.Type != "" {
		cfg.Metrics.Type = yamlConfig.Metrics.Type
	}
	for k, v := range yamlConfig.Metrics.Config {
		cfg.Metrics.Config[k] = v
	}

	cfg.Variables = yamlConfig.Variables

	return nil
}

func validateConfig(cfg *config.Config) error {
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	for i, target := range cfg.Targets {
		if err := target.Validate(); err != nil {
			return fmt.Errorf("target[%d] validation failed: %w", i, err)
		}
	}

	duration, err := time.ParseDuration(cfg.Load.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration '%s': %w", cfg.Load.Duration, err)
	}
	if duration < time.Second {
		return fmt.Errorf("duration must be at least 1 second")
	}

	if cfg.Engine == "http" {
		for i, target := range cfg.Targets {
			if err := validateHTTPTarget(target); err != nil {
				return fmt.Errorf("target[%d]: %w", i, err)
			}
		}
	}

	return nil
}

func validateHTTPTarget(target config.Target) error {
	httpTarget, ok := target.(*config.HTTPTargetConfig)
	if !ok {
		return fmt.Errorf("expected HTTP target config for http engine")
	}

	parsedURL, err := url.Parse(httpTarget.URL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must contain a valid hostname")
	}

	if !strings.Contains(hostname, ".svc.") {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: hostname '%s' doesn't look like a Kubernetes service (missing .svc.)\n", hostname)
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname '%s': %w (ensure it's a valid cluster-internal service)", hostname, err)
	}

	hasPrivateIP := false
	for _, ip := range ips {
		if isPrivateIP(ip) {
			hasPrivateIP = true
			break
		}
	}

	if !hasPrivateIP {
		return fmt.Errorf("security: target '%s' must resolve to a private IP address (cluster-internal services only)", hostname)
	}

	return nil
}

type stringSlice []string

func (s *stringSlice) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",     // Private network
		"172.16.0.0/12",  // Private network
		"192.168.0.0/16", // Private network
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addresses
	}

	for _, cidr := range privateRanges {
		_, subnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}
