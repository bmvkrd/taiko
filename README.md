<p align="center">
  <a href="https://github.com/yourname/taiko">
    <img src="assets/logo.png" width="250" />
  </a>
</p>

<h1 align="center">Taiko</h1>


A high-performance load testing engine designed for standalone (CLI) or Kubernetes cluster-internal use.

Supports load generation with a pluggable architecture for HTTP, gRPC and Kafka engines.

## Configuration file

Configuration is loaded from a YAML file. The file location is resolved in priority order:

1. `--config-file` CLI flag
2. `CONFIG_FILE` environment variable
3. Default path: `/etc/taiko/config.yaml`

### Top-level parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `engine` | string | Engine type: `http`, `grpc`, or `kafka`. |
| `targets` | list | One or more target definitions (fields depend on engine type). |
| `load.duration` | duration | Total duration of the load test (e.g. `30s`, `5m`, `1h`). |
| `metrics.type` | string | Metrics output destination: `console`, `prometheus`, or `s3`. |
| `variables` | list | Dynamic variables for template substitution in payloads. |

## HTTP Engine

The HTTP engine sends requests to one or more HTTP endpoints. Multiple targets are supported simultaneously, each with its own URL, method, headers, body, and target RPS. A worker pool is automatically scaled up or down every 2 seconds to match the configured RPS across all targets, with weighted target selection proportional to each target's RPS share.

### HTTP target fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Target URL. Supports `{{variable}}` template substitution. |
| `method` | string | no | HTTP method (e.g. `GET`, `POST`). Defaults to `GET`. |
| `timeout` | duration | no | Per-request timeout (e.g. `5s`). |
| `body` | string | no | Request body. Supports `{{variable}}` template substitution. |
| `headers` | map | no | HTTP headers as key-value pairs. |
| `rps` | int | yes | Target requests per second for this endpoint. |
| `burst` | int | no | Token bucket burst size for the rate limiter (default: 1). |

### Example HTTP configuration

```yaml
engine: http
targets:
  - url: http://localhost:8081/api/endpoint?id={{id}}
    method: POST
    timeout: 5s
    body: '{}'
    headers:
      Content-Type: application/json
    rps: 500
load:
  duration: 1m
metrics:
  type: console
variables:
  - name: id
    type: uuid
```

## gRPC Engine

The gRPC engine calls unary RPC methods on one or more gRPC services. Service schema is resolved either from `.proto` source files (compiled at startup) or via gRPC server reflection when no proto files are provided. Multiple targets are supported, each pointing to a different endpoint or method, with workers automatically scaled to hit the configured RPS.

### gRPC target fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `endpoint` | string | yes | gRPC server address (e.g. `localhost:9090`). |
| `service` | string | yes | Fully-qualified service name (e.g. `mypackage.MyService`). |
| `method` | string | yes | RPC method name (e.g. `SayHello`). |
| `proto_files` | list | no | Paths to `.proto` files for schema resolution. Falls back to server reflection if omitted. |
| `payload` | string | no | JSON request payload. Supports `{{variable}}` template substitution. |
| `metadata` | map | no | gRPC metadata headers as key-value pairs. Values support `{{variable}}` substitution. |
| `timeout` | duration | no | Per-request timeout (default: `30s`). |
| `rps` | int | yes | Target requests per second. |
| `burst` | int | no | Token bucket burst size for the rate limiter (default: 1). |

### Example gRPC configuration

```yaml
engine: grpc
targets:
  - endpoint: localhost:9090
    service: taiko.TaikoService
    method: Taiko
    proto_files:
      - examples/taiko.proto
    payload: '{"message": "{{user_id}}"}'
    metadata:
      x-request-id: "{{uuid}}"
    rps: 10
    timeout: 5s
load:
  duration: 60s
variables:
  - name: user_id
    type: uuid
```

## Kafka Engine

The Kafka engine produces messages to one or more Kafka topics. A single shared producer client is used across all targets, with broker addresses pooled from all target definitions. Multiple targets are supported, each with its own topic, key, value template, and RPS. Workers are automatically scaled to match the configured message throughput.

### Kafka target fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `brokers` | list | yes | One or more Kafka broker addresses (e.g. `["localhost:9092"]`). |
| `topic` | string | yes | Kafka topic to produce messages to. |
| `key` | string | no | Message key template. Supports `{{variable}}` substitution. |
| `value` | string | no | Message value template. Supports `{{variable}}` substitution. |
| `headers` | map | no | Kafka record headers as key-value pairs. Values support `{{variable}}` substitution. |
| `rps` | int | yes | Target messages per second. |
| `burst` | int | no | Token bucket burst size for the rate limiter (default: 1). |

### Example Kafka configuration

```yaml
engine: kafka
targets:
  - brokers: ["localhost:9092"]
    topic: taiko
    key: "{{user_id}}"
    value: '{"event": "click", "user": "{{user_id}}", "timestamp": "{{timestamp}}"}'
    rps: 5
load:
  duration: 10s
variables:
  - name: user_id
    type: uuid
  - name: timestamp
    type: timestamp
    generator:
      format: rfc3339
```

## Dynamic Variables

Variables let you inject dynamic values into request payloads at runtime. They are defined under the top-level `variables` key and referenced anywhere in a payload using `{{variable_name}}` placeholders. Substitution is applied per-request and is supported across all engines:

- **HTTP**: `url` and `body` fields
- **gRPC**: `payload` and `metadata` values
- **Kafka**: `key`, `value`, and `headers` values

Each variable has a `name`, a `type`, and an optional `generator` block with type-specific parameters.

### Variable types

| Type | Description | Generator fields |
|------|-------------|-----------------|
| `uuid` | Random UUID v4 generated per request. | _(none)_ |
| `timestamp` | Current time at request time. | `format`: `unix` (default) or `rfc3339` |
| `string_set` | Selects a value from a fixed list of strings. | `values` (list of strings), `mode`: `seq` or `rnd` |
| `int_range` | Integer within a min/max range (inclusive). | `min` (int), `max` (int), `mode`: `seq` or `rnd` |
| `int_set` | Selects an integer from a fixed list. | `values` (list of ints), `mode`: `seq` or `rnd` |

`mode: seq` cycles through values sequentially (thread-safe); `mode: rnd` picks randomly on each request.
