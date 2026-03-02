FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /taiko ./cmd/main.go

FROM scratch
COPY --from=builder /taiko /taiko
ENTRYPOINT ["/taiko"]
