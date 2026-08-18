# Go-API

A simple REST API for managing orders, built with Go, the [chi](https://github.com/go-chi/chi) router, and Redis for storage.

## Features

- Create, list, read, update, and delete orders
- Cursor-based pagination for listing orders
- Order status transitions (`shipped`, `completed`)
- JSON request and response bodies
- Redis-backed persistence
- Graceful shutdown on interrupt
- Prometheus metrics for HTTP, Redis, and order activity

## Requirements

- **With Docker:** only [Docker](https://docs.docker.com/get-docker/) (with Docker Compose)
- **Without Docker:** Go 1.26 or newer and a running Redis instance

## Getting started

### Using Docker Compose (recommended)

This starts both the app and Redis together:

```sh
docker compose up -d --build
```

The server is then available at `http://localhost:3000`. Stop it with:

```sh
docker compose down
```

### Using the published image

Pull the pre-built image from Docker Hub and point it at your Redis:

```sh
docker run -p 3000:3000 \
  -e REDIS_ADDR=<your-redis-host>:6379 \
  helsinki50/go-api:latest
```

### Without Docker

1. Start Redis (defaults to `localhost:6379`).
2. Run the server:

   ```sh
   go run .
   ```

The server listens on port `3000` by default.

## Docker

The project ships with a multi-stage [`Dockerfile`](Dockerfile) that produces a small (~10 MB) static image, plus a [`docker-compose.yml`](docker-compose.yml) that wires it to Redis.

- Build the image:

  ```sh
  docker build -t go-api .
  ```

- The image runs a single static binary as a non-root user, and everything is configured via environment variables (see [Configuration](#configuration)).

### Docker Hub

Images are published to Docker Hub as [`helsinki50/go-api`](https://hub.docker.com/r/helsinki50/go-api):

```sh
docker pull helsinki50/go-api:latest
```

### Continuous delivery

A GitHub Actions workflow ([`.github/workflows/docker-publish.yml`](.github/workflows/docker-publish.yml)) builds and pushes the image automatically on every push to `main` and on every `v*` tag.

Tags generated:

| Trigger | Tag |
| ------- | --- |
| push to `main` | `latest` |
| tag `vX.Y.Z` | `vX.Y.Z` |
| any push | `sha-<commit-hash>` |

The workflow requires two secrets in GitHub → **Settings → Secrets and variables → Actions**:

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

## Configuration

Configuration is read from environment variables.

| Variable      | Description            | Default         |
| ------------- | ---------------------- | --------------- |
| `REDIS_ADDR`  | Redis server address   | `localhost:6379` |
| `SERVER_PORT` | HTTP server port       | `3000`           |

Example:

```sh
REDIS_ADDR=localhost:6379 SERVER_PORT=8080 go run .
```

## API

### Health check

`GET /`

Returns `200 OK` with an empty body.

### Create an order

`POST /orders`

Request body:

```json
{
  "customer_id": "4e9e2c8a-6f63-4c2d-9e51-9d7c5e5f4a1a",
  "line_items": [
    {
      "item_id": "7b8e6f2d-3c4b-4a1a-9b6d-2f8d1e6c4a3b",
      "quantity": 2,
      "price": 2500
    }
  ]
}
```

Response: `201 Created` with the created order, including the generated `order_id` and `created_at` timestamp.

### List orders

`GET /orders`

Optional query parameter:

- `cursor`: pagination cursor returned as `next` from a previous response (defaults to `0`)

Response:

```json
{
  "items": [],
  "next": 0
}
```

`next` is omitted when there are no more items.

### Get an order

`GET /orders/{id}`

Response: `200 OK` with the order, or `404 Not Found` if it does not exist.

### Update order status

`PUT /orders/{id}`

Request body:

```json
{
  "status": "shipped"
}
```

Allowed status values:

- `shipped`: sets `shipped_at`
- `completed`: sets `completed_at` (only valid after the order has been shipped)

Response: `200 OK` with the updated order.

### Delete an order

`DELETE /orders/{id}`

Response: `200 OK` on success, or `404 Not Found` if the order does not exist.

## Data model

```json
{
  "order_id": 123456789,
  "customer_id": "4e9e2c8a-6f63-4c2d-9e51-9d7c5e5f4a1a",
  "line_items": [
    {
      "item_id": "7b8e6f2d-3c4b-4a1a-9b6d-2f8d1e6c4a3b",
      "quantity": 2,
      "price": 2500
    }
  ],
  "created_at": "2026-08-18T12:00:00Z",
  "shipped_at": null,
  "completed_at": null
}
```

## Metrics

Metrics are exposed in the Prometheus text format at `GET /metrics`.

### HTTP

| Metric                          | Type      | Labels                        |
| ------------------------------- | --------- | ----------------------------- |
| `http_requests_total`           | counter   | `method`, `route`, `status`   |
| `http_request_duration_seconds` | histogram | `method`, `route`, `status`   |
| `http_requests_in_flight`       | gauge     |                               |
| `http_requests_per_second`      | gauge     |                               |

The `route` label uses the route pattern (for example `/orders/{id}`) rather than individual IDs.

### Redis

| Metric                            | Type      | Labels    |
| --------------------------------- | --------- | --------- |
| `redis_command_duration_seconds`  | histogram | `command` |
| `redis_command_errors_total`      | counter   | `command` |
| `redis_pool_hits_total`           | counter   |           |
| `redis_pool_misses_total`         | counter   |           |
| `redis_pool_timeouts_total`       | counter   |           |
| `redis_pool_conns`                | gauge     |           |
| `redis_pool_idle_conns`           | gauge     |           |
| `redis_pool_stale_conns`          | counter   |           |

### Orders

| Metric                     | Type    | Labels    |
| -------------------------- | ------- | --------- |
| `orders_created_total`     | counter |           |
| `orders_create_errors_total` | counter |         |
| `orders_listed_total`      | counter |           |
| `orders_lookup_total`      | counter | `result` (`found`, `not_found`) |
| `orders_updated_total`     | counter | `status` (`shipped`, `completed`) |
| `orders_deleted_total`     | counter |           |
| `orders_created_per_second` | gauge   |           |

### Request rate

RPS can be derived from the raw counters with PromQL:

- `sum(rate(http_requests_total[1m]))` — overall requests per second
- `sum by (route) (rate(http_requests_total[1m]))` — requests per second per route
- `sum by (status) (rate(http_requests_total[5m]))` — requests per second per status code
- `rate(orders_created_total[1m])` — orders created per second

The exported `*_per_second` gauges are exponentially weighted moving averages with a 1 second time constant. They provide a plain number directly from `/metrics`, which is useful when PromQL is not available. For precise per-second rates over a fixed window, prefer the `rate()` queries above.

### Go runtime

The standard Go and process collectors are registered automatically, so `go_*` and `process_*` metrics (goroutines, memory, GC, CPU, file descriptors) are also exposed at `/metrics`.

## Project structure

```
.
├── .github/
│   └── workflows/
│       └── docker-publish.yml   # CI: build & push image on push/tag
├── application/                 # App setup, config loading, and route registration
│   ├── app.go
│   ├── config.go
│   └── routes.go
├── handler/                     # HTTP handlers
│   └── order.go
├── metrics/                     # Prometheus collectors and middleware
│   └── metrics.go
├── model/                       # Data models
│   └── order.go
├── repository/
│   └── order/                   # Redis repository
│       └── redis.go
├── Dockerfile                   # Multi-stage image build
├── docker-compose.yml           # App + Redis stack
├── .dockerignore                # Build-context exclusions
├── .gitignore                   # Version-control exclusions
├── main.go                      # Entrypoint
├── go.mod
└── go.sum
```

## How storage works

Each order is stored in Redis as a JSON string under the key `order:{id}`, and the key is added to the `orders` set. Listing orders scans the `orders` set with a cursor and fetches the orders in bulk.
