
# VectorHub

A high-performance distributed vector database system designed for scale. VectorHub shards Redis for speed, exposes a gRPC interface for fast insert/search operations, and replicates cleanly for high availability. Stress-tested to handle over **1 million vector writes per minute** while keeping lookups under **100ms**.

## Features

- **Horizontal Scaling**: Consistent hashing-based sharding across multiple Redis instances
- **High Performance**: Optimized for 1M+ vector operations/minute with sub-100ms search latency
- **Replication**: Built-in replication with automatic failover and lag monitoring
- **gRPC API**: Fast binary protocol with streaming support
- **Multiple Distance Metrics**: Cosine similarity, Euclidean distance, and dot product
- **Monitoring**: Prometheus metrics and health endpoints
- **Production Ready**: Docker support, comprehensive testing, and operational tooling

## Quick Start

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- Redis (for local development)

### Installation

```bash
# Clone the repository
git clone https://github.com/elcruzo/vectorhub
cd vectorhub

# Start with Docker Compose (recommended)
docker-compose up -d

# Or build and run locally
make build
./bin/vectorhub -config configs/config.yaml
```

### Basic Usage

```go
package main

import (
    "context"
    "log"
  
    "github.com/elcruzo/vectorhub/pkg/client"
)

func main() {
    // Connect to VectorHub
    client, err := client.NewClient(&client.Config{
        Address: "localhost:50051",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()
  
    ctx := context.Background()
  
    // Create an index
    err = client.CreateIndex(ctx, client.CreateIndexOptions{
        Name:         "embeddings",
        Dimension:    128,
        Metric:       "cosine",
        ShardCount:   8,
        ReplicaCount: 2,
    })
  
    // Insert a vector
    err = client.Insert(ctx, "embeddings", "doc-1", 
        []float32{0.1, 0.2, 0.3, /* ... */}, 
        map[string]string{"category": "document"})
  
    // Search for similar vectors
    results, err := client.Search(ctx, "embeddings", 
        []float32{0.1, 0.2, 0.3, /* ... */}, 
        client.SearchOptions{
            TopK: 10,
            IncludeMetadata: true,
        })
  
    for _, result := range results {
        log.Printf("ID: %s, Score: %f", result.ID, result.Score)
    }
}
```

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   gRPC Client   │    │   gRPC Client   │    │   gRPC Client   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────┐
                    │  VectorHub      │
                    │  gRPC Server    │
                    └─────────────────┘
                                 │
                    ┌─────────────────┐
                    │  Shard Manager  │
                    │ (Consistent Hash)│
                    └─────────────────┘
                                 │
        ┌────────────┬────────────┼────────────┬────────────┐
        │            │            │            │            │
   ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐
   │ Redis 0 │  │ Redis 1 │  │ Redis 2 │  │ Redis 3 │  │ Redis N │
   │ Primary │  │ Replica │  │ Primary │  │ Replica │  │   ...   │
   └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘
```

### Key Components

- **Vector Service**: Core gRPC service handling CRUD operations
- **Shard Manager**: Routes requests using consistent hashing
- **Replication Manager**: Handles primary-replica synchronization
- **Storage Layer**: Redis adapter with connection pooling
- **Metrics Collector**: Prometheus metrics for monitoring

## Performance

### Benchmarks

- **Insert Throughput**: 1M+ vectors/minute
- **Search Latency**: <100ms for 99th percentile
- **Batch Operations**: 10K+ vectors per batch
- **Memory Efficiency**: <1KB overhead per vector

### Optimization Features

- Connection pooling and keepalive
- Parallel batch operations
- Efficient vector serialization
- Query result caching
- Background health monitoring

## Configuration

### Basic Configuration (config.yaml)

```yaml
server:
  grpc_port: 50051
  metrics_port: 9090

redis:
  addresses:
    - "localhost:6379"
    - "localhost:6380"
  password: ""
  pool_size: 100

sharding:
  shard_count: 8
  replica_count: 2
  virtual_nodes: 150

replication:
  factor: 2
  sync_interval_seconds: 5
  async_replication: true

metrics:
  enabled: true
  namespace: "vectorhub"
```

### Environment Variables

All configuration options can be overridden with environment variables:

```bash
export VECTORHUB_REDIS__ADDRESSES="redis1:6379,redis2:6379"
export VECTORHUB_SHARDING__SHARD_COUNT=16
export VECTORHUB_LOGGING__LEVEL=debug
```

## API Reference

### gRPC Service Methods

- `Insert(vector)` - Insert a single vector
- `BatchInsert(vectors)` - Insert multiple vectors in parallel
- `Search(query, options)` - Find similar vectors
- `Get(id)` - Retrieve a vector by ID
- `Update(id, vector)` - Update an existing vector
- `Delete(id)` - Delete a vector
- `CreateIndex(options)` - Create a new vector index
- `DropIndex(name)` - Delete an index
- `GetStats(index)` - Get index statistics

### Distance Metrics

- **Cosine Similarity**: `cosine` (default for normalized vectors)
- **Euclidean Distance**: `euclidean` (good for spatial data)
- **Dot Product**: `dot_product` (fast for high-dimensional data)

## Monitoring

### Metrics

VectorHub exposes Prometheus metrics on `/metrics`:

- `vectorhub_vectors_inserted_total`
- `vectorhub_searches_total`
- `vectorhub_latency_search_seconds`
- `vectorhub_shards_status`
- `vectorhub_replication_lag_seconds`

### Health Checks

- gRPC health checks: Use `grpc_health_probe`
- HTTP health endpoint: `GET /health` on metrics port
- Shard health monitoring with automatic failover

## Development

### Building

```bash
# Install dependencies
make install-tools

# Generate protobuf code
make proto

# Run tests
make test

# Build binary
make build

# Run benchmarks
make benchmark
```

### Testing

```bash
# Unit tests
make test-unit

# Integration tests (requires Redis)
make test-integration

# Benchmark tests
make test-benchmark

# Coverage report
make coverage
```

### Docker Development

```bash
# Build Docker image
make docker-build

# Run with Docker Compose
docker-compose up -d

# View logs
docker-compose logs -f vectorhub

# Scale Redis instances
docker-compose up -d --scale redis=6
```

## Production Deployment

### Docker Compose

The included `docker-compose.yml` provides a production-ready setup with:

- 4 Redis instances for sharding
- VectorHub server
- Prometheus for metrics
- Grafana for dashboards

### Kubernetes

Deploy to Kubernetes using the provided manifests:

```bash
kubectl apply -f deployments/k8s/
```

### Scaling Considerations

- **Horizontal Scaling**: Add more Redis shards
- **Vertical Scaling**: Increase memory and CPU resources
- **Replication**: Increase replica count for higher availability
- **Load Balancing**: Use multiple VectorHub instances behind a load balancer

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines

- Follow Go best practices and `gofmt` formatting
- Write comprehensive tests for new features
- Update documentation for API changes
- Benchmark performance-critical code

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Roadmap

- [ ] Vector compression and quantization
- [ ] GPU acceleration for similarity computation
- [ ] Graph-based indexing (HNSW)
- [ ] Multi-tenant isolation
- [ ] REST API gateway
- [ ] Vector analytics and insights

## Support

- **Issues**: [GitHub Issues](https://github.com/elcruzo/vectorhub/issues)
- **Discussions**: [GitHub Discussions](https://github.com/elcruzo/vectorhub/discussions)
- **Documentation**: [Wiki](https://github.com/elcruzo/vectorhub/wiki)

---

**VectorHub** - Built for scale, optimized for speed, designed for reliability.
