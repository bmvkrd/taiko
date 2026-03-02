package metrics

import (
	"context"
)

type S3Connector struct {
	bucket string
	prefix string
	region string
}

func NewS3Connector() Connector {
	return &S3Connector{}
}

func (s *S3Connector) Init(config map[string]string) error {
	s.bucket = config["bucket"]
	s.prefix = config["prefix"]
	s.region = config["region"]
	if s.region == "" {
		s.region = "us-east-1"
	}
	return nil
}

func (s *S3Connector) OnRequest(ctx context.Context, metrics *RequestMetrics) error {
	return nil
}

func (s *S3Connector) OnInterval(ctx context.Context, stats *IntervalStats) error {
	return nil
}

func (s *S3Connector) OnComplete(ctx context.Context, summary *Summary) error {
	// TODO: Upload summary as JSON to S3
	return nil
}

func (s *S3Connector) Close() error {
	return nil
}
