package metrics

import (
	"context"
)

type PrometheusConnector struct {
	pushGatewayURL string
	jobName        string
}

func NewPrometheusConnector() Connector {
	return &PrometheusConnector{}
}

func (p *PrometheusConnector) Init(config map[string]string) error {
	p.pushGatewayURL = config["pushgateway_url"]
	p.jobName = config["job_name"]
	if p.jobName == "" {
		p.jobName = "load_test"
	}
	return nil
}

func (p *PrometheusConnector) OnRequest(ctx context.Context, metrics *RequestMetrics) error {
	return nil
}

func (p *PrometheusConnector) OnInterval(ctx context.Context, stats *IntervalStats) error {
	// TODO: Push metrics to Prometheus Pushgateway
	return nil
}

func (p *PrometheusConnector) OnComplete(ctx context.Context, summary *Summary) error {
	// TODO: Push final summary metrics to Prometheus Pushgateway
	return nil
}

func (p *PrometheusConnector) Close() error {
	return nil
}
