# Taiko Load Tester

Run load tests of any complexity using Taiko by generating a config file and executing the runner.

## What this skill does

1. Collects load test requirements from the user
2. Generates a valid Taiko YAML config file
3. Builds the runner if the binary is missing
4. Executes the test and streams live output

---

## How to use

Invoke this skill by typing `/taiko-load-tester` followed by a description of the load test you want to run. You may supply as much or as little detail as you like — defaults are provided for everything optional.

**Examples:**

```
/taiko-load-tester POST http://my-service.default.svc.cluster.local/api/users at 500 RPS for 2m
/taiko-load-tester hammer the search endpoint with random UUIDs, 1000 RPS, 10 minutes
/taiko-load-tester grpc my-service.default.svc.cluster.local:9090 service=Search method=Query 200 RPS 30s
/taiko-load-tester kafka broker=kafka.default.svc.cluster.local:9092 topic=events 1000 RPS 5m
/taiko-load-tester multi-target: GET /health 100 RPS + POST /process 50 RPS, 1 minute
```

---

## Instructions for the AI

You are a Taiko load-test assistant. When invoked, follow these steps precisely.

### Step 1 — Parse the request

Extract these parameters from the user's message. Use the defaults when a value is not specified.

| Parameter | Default | Notes |
|-----------|---------|-------|
| `engine` | `http` | `http`, `grpc`, or `kafka` |
| `duration` | `60s` | Any Go duration: `30s`, `5m`, `1h` |
| `metrics` | `console` | `console`, `prometheus`, or `s3` |
| `config_path` | `/tmp/taiko-load-test.yaml` | Where to write the generated config |

For each **target** (one or more), extract:

*HTTP targets:*
| Field | Default | Notes |
|-------|---------|-------|
| `url` | *(required)* | Must be a private/cluster-internal address |
| `method` | `GET` | HTTP verb |
| `rps` | `100` | Requests per second for this target |
| `timeout` | `5s` | Per-request timeout |
| `body` | *(none)* | JSON body string; omit if empty |
| `headers` | *(none)* | Key/value map |

*gRPC targets:*
| Field | Default | Notes |
|-------|---------|-------|
| `endpoint` | *(required)* | `host:port` |
| `service` | *(required)* | Fully-qualified service name |
| `method` | *(required)* | RPC method name |
| `rps` | `100` | |
| `timeout` | `5s` | |
| `payload` | `{}` | JSON string |
| `proto_files` | *(none)* | List of `.proto` file paths |
| `metadata` | *(none)* | Key/value map |

*Kafka targets:*
| Field | Default | Notes |
|-------|---------|-------|
| `brokers` | *(required)* | List of `host:port` |
| `topic` | *(required)* | Kafka topic name |
| `rps` | `100` | Messages per second |
| `key` | *(none)* | Message key (supports `{{variables}}`) |
| `value` | `{}` | Message value (supports `{{variables}}`) |

**Variable inference rules:**
- If the URL/body/payload/key/value contains `{{uuid}}` or words like "random id", "unique id" → add a `uuid` variable named `id`.
- If the user mentions sequential or random integers → add an `int_range` variable with appropriate min/max and mode (`seq` or `rnd`).
- If the user mentions a fixed set of integer values → add an `int_set` variable.
- If the user mentions a fixed set of string values (e.g., regions, statuses, environments) → add a `string_set` variable.
- If the user mentions timestamps, event times, or "current time" → add a `timestamp` variable; prefer `rfc3339` format for JSON bodies and `unix` for numeric fields.
- Always name variables to match the `{{placeholder}}` used in the template strings.

---

### Step 2 — Generate the YAML config

Use the Write tool to create the config file at `config_path`.

**HTTP example (single target):**
```yaml
engine: http
targets:
  - url: http://my-service.default.svc.cluster.local/api/endpoint
    method: POST
    timeout: 5s
    body: '{"id": "{{request_id}}"}'
    headers:
      Content-Type: application/json
    rps: 500
load:
  duration: 2m
metrics:
  type: console
variables:
  - name: request_id
    type: uuid
```

**HTTP example (multi-target):**
```yaml
engine: http
targets:
  - url: http://my-service.default.svc.cluster.local/health
    method: GET
    timeout: 5s
    rps: 100
  - url: http://my-service.default.svc.cluster.local/process
    method: POST
    timeout: 10s
    body: '{"job": "{{job_id}}"}'
    headers:
      Content-Type: application/json
    rps: 50
load:
  duration: 1m
metrics:
  type: console
variables:
  - name: job_id
    type: uuid
```

**gRPC example:**
```yaml
engine: grpc
targets:
  - endpoint: my-service.default.svc.cluster.local:9090
    service: mypackage.MyService
    method: DoWork
    payload: '{"user": "{{user_id}}"}'
    metadata:
      x-request-id: "{{req_id}}"
    rps: 200
    timeout: 5s
load:
  duration: 30s
metrics:
  type: console
variables:
  - name: user_id
    type: uuid
  - name: req_id
    type: uuid
```

**Kafka example:**
```yaml
engine: kafka
targets:
  - brokers: ["kafka.default.svc.cluster.local:9092"]
    topic: my-events
    key: "{{user_id}}"
    value: '{"event": "click", "user": "{{user_id}}", "seq": {{seq_num}}}'
    rps: 1000
load:
  duration: 5m
metrics:
  type: console
variables:
  - name: user_id
    type: uuid
  - name: seq_num
    type: int_range
    generator:
      min: 1
      max: 100000
      mode: seq
```

**Variable types reference:**
```yaml
# UUID v4 — no generator block needed
- name: my_id
  type: uuid

# Random integer in a range
- name: page
  type: int_range
  generator:
    min: 1
    max: 100
    mode: rnd   # "seq" for sequential, "rnd" for random

# Fixed set of integers
- name: status_code
  type: int_set
  generator:
    values: [200, 201, 400, 404, 500]
    mode: rnd   # "seq" or "rnd"

# Fixed set of strings
- name: region
  type: string_set
  generator:
    values: ["us-east-1", "eu-west-1", "ap-southeast-1"]
    mode: rnd   # "seq" or "rnd"

# Current timestamp — emits a new value on every request
- name: event_time
  type: timestamp
  generator:
    format: rfc3339   # "rfc3339" → string  e.g. "2026-03-06T12:00:00Z"
                      # "unix"    → int64   e.g. 1741262400
```

Only include the `variables` section if at least one variable is used.

---

### Step 3 — Locate or build the binary

Check whether `bin/taiko-runner` exists in the current working directory:

```bash
ls bin/taiko-runner 2>/dev/null && echo "exists" || echo "missing"
```

If it is **missing**, build it first:

```bash
make build
```

If `make build` fails, try the direct Go build:

```bash
go build -o bin/taiko-runner ./cmd/...
```

---

### Step 4 — Run the load test

Execute the runner with the generated config:

```bash
./bin/taiko-runner --config-file <config_path>
```

Stream the output to the user. The runner prints a live ticker showing:
- Current actual RPS vs target RPS
- Number of active workers (auto-scaled)
- Error rate and latency percentiles (p50, p95, p99)
- Elapsed time and progress

---

### Step 5 — Summarise results

After the run completes (or is interrupted), parse the final summary block printed by the runner and present the key numbers to the user:

- Total requests sent
- Success rate (2xx responses)
- Peak RPS achieved
- Latency percentiles (p50, p95, p99)
- Error breakdown (if any)

If the test failed to start, diagnose the error:
- Config validation failure → show the relevant YAML and explain the problem
- DNS resolution failure → remind the user that HTTP targets must resolve to private/cluster-internal IPs
- Binary not found after build → show the build error

---

### Complexity presets

When the user says phrases like "light", "medium", "heavy", or "stress test" without explicit RPS values, apply these presets as a starting point and ask the user to confirm before running:

| Preset | RPS | Duration | Workers behaviour |
|--------|-----|----------|-------------------|
| Smoke / sanity | 10 | 30s | Minimal load, verifies endpoint is up |
| Light | 100 | 1m | Baseline performance |
| Medium | 500 | 5m | Typical production-like load |
| Heavy | 2 000 | 10m | High-concurrency stress |
| Soak | 500 | 1h | Sustained load to find memory leaks |
| Stress / spike | 10 000 | 2m | Push to breaking point |

Always state the preset values clearly before writing the config so the user can adjust them.

---

### Safety rules

- **Never** modify an existing production config file; always write to a temporary path (e.g., `/tmp/taiko-*.yaml`) unless the user explicitly provides a different output path.
- Remind the user that the HTTP engine **enforces private-IP-only targets** — public URLs will be rejected by the runner.
- If `duration` would exceed 1 hour, ask the user to confirm before proceeding.
- If total RPS across all targets exceeds 50 000, warn the user and ask for confirmation.
