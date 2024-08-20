package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Collector struct {
	vectorsInserted prometheus.Counter
	vectorsDeleted  prometheus.Counter
	searches        prometheus.Counter
	indexesCreated  prometheus.Counter
	indexesDropped  prometheus.Counter

	insertLatency      prometheus.Histogram
	searchLatency      prometheus.Histogram
	batchInsertLatency prometheus.Histogram

	activeConnections prometheus.Gauge
	vectorCount       prometheus.Gauge
	indexCount        prometheus.Gauge
	memoryUsage       prometheus.Gauge

	shardStatus    *prometheus.GaugeVec
	replicationLag *prometheus.GaugeVec
	cacheHitRate   prometheus.Gauge

	errorCounter   *prometheus.CounterVec
	requestCounter *prometheus.CounterVec

	customMetrics map[string]prometheus.Metric
}

func NewCollector() *Collector {
	return &Collector{
		vectorsInserted: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "vectorhub",
			Subsystem: "vectors",
			Name:      "inserted_total",
			Help:      "Total number of vectors inserted",
		}),

		vectorsDeleted: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "vectorhub",
			Subsystem: "vectors",
			Name:      "deleted_total",
			Help:      "Total number of vectors deleted",
		}),

		searches: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "vectorhub",
			Subsystem: "operations",
			Name:      "searches_total",
			Help:      "Total number of search operations",
		}),

		indexesCreated: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "vectorhub",
			Subsystem: "indexes",
			Name:      "created_total",
			Help:      "Total number of indexes created",
		}),

		indexesDropped: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "vectorhub",
			Subsystem: "indexes",
			Name:      "dropped_total",
			Help:      "Total number of indexes dropped",
		}),

		insertLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "vectorhub",
			Subsystem: "latency",
			Name:      "insert_seconds",
			Help:      "Insert operation latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),

		searchLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "vectorhub",
			Subsystem: "latency",
			Name:      "search_seconds",
			Help:      "Search operation latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),

		batchInsertLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "vectorhub",
			Subsystem: "latency",
			Name:      "batch_insert_seconds",
			Help:      "Batch insert operation latency in seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50},
		}),

		activeConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "vectorhub",
			Subsystem: "connections",
			Name:      "active",
			Help:      "Number of active client connections",
		}),

		vectorCount: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "vectorhub",
			Subsystem: "storage",
			Name:      "vector_count",
			Help:      "Total number of vectors stored",
		}),

		indexCount: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "vectorhub",
			Subsystem: "storage",
			Name:      "index_count",
			Help:      "Total number of indexes",
		}),

		memoryUsage: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "vectorhub",
			Subsystem: "system",
			Name:      "memory_usage_bytes",
			Help:      "Memory usage in bytes",
		}),

		shardStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "vectorhub",
				Subsystem: "shards",
				Name:      "status",
				Help:      "Status of each shard (1=healthy, 0=unhealthy)",
			},
			[]string{"shard_id", "node"},
		),

		replicationLag: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "vectorhub",
				Subsystem: "replication",
				Name:      "lag_seconds",
				Help:      "Replication lag in seconds",
			},
			[]string{"shard_id", "replica"},
		),

		cacheHitRate: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "vectorhub",
			Subsystem: "cache",
			Name:      "hit_rate",
			Help:      "Cache hit rate (0-1)",
		}),

		errorCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "vectorhub",
				Subsystem: "errors",
				Name:      "total",
				Help:      "Total errors by operation",
			},
			[]string{"operation", "error_type"},
		),

		requestCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "vectorhub",
				Subsystem: "requests",
				Name:      "total",
				Help:      "Total requests by operation",
			},
			[]string{"operation", "status"},
		),

		customMetrics: make(map[string]prometheus.Metric),
	}
}

func (c *Collector) IncrementCounter(name string, value float64) {
	switch name {
	case "vectors_inserted":
		c.vectorsInserted.Add(value)
	case "vectors_deleted":
		c.vectorsDeleted.Add(value)
	case "searches":
		c.searches.Add(value)
	case "indexes_created":
		c.indexesCreated.Add(value)
	case "indexes_dropped":
		c.indexesDropped.Add(value)
	}
}

func (c *Collector) RecordLatency(operation string, duration time.Duration) {
	seconds := duration.Seconds()

	switch operation {
	case "insert":
		c.insertLatency.Observe(seconds)
	case "search":
		c.searchLatency.Observe(seconds)
	case "batch_insert":
		c.batchInsertLatency.Observe(seconds)
	}

	c.requestCounter.WithLabelValues(operation, "success").Inc()
}

func (c *Collector) SetGauge(name string, value float64) {
	switch name {
	case "active_connections":
		c.activeConnections.Set(value)
	case "vector_count":
		c.vectorCount.Set(value)
	case "index_count":
		c.indexCount.Set(value)
	case "memory_usage":
		c.memoryUsage.Set(value)
	case "cache_hit_rate":
		c.cacheHitRate.Set(value)
	}
}

func (c *Collector) UpdateShardStatus(shardID string, node string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	c.shardStatus.WithLabelValues(shardID, node).Set(value)
}

func (c *Collector) UpdateReplicationLag(shardID string, replica string, lagSeconds float64) {
	c.replicationLag.WithLabelValues(shardID, replica).Set(lagSeconds)
}

func (c *Collector) IncrementError(operation string, errorType string) {
	c.errorCounter.WithLabelValues(operation, errorType).Inc()
	c.requestCounter.WithLabelValues(operation, "error").Inc()
}

func (c *Collector) RecordBatchOperation(operation string, batchSize int, duration time.Duration, failures int) {
	successes := batchSize - failures

	c.requestCounter.WithLabelValues(operation, "success").Add(float64(successes))
	if failures > 0 {
		c.requestCounter.WithLabelValues(operation, "error").Add(float64(failures))
	}

	seconds := duration.Seconds()
	perItemLatency := seconds / float64(batchSize)

	for i := 0; i < successes; i++ {
		c.insertLatency.Observe(perItemLatency)
	}
}

type MetricsSnapshot struct {
	VectorsInserted   float64
	VectorsDeleted    float64
	Searches          float64
	ActiveConnections float64
	VectorCount       float64
	MemoryUsage       float64
	CacheHitRate      float64
}

func (c *Collector) GetSnapshot() *MetricsSnapshot {
	// Note: Prometheus metrics are pull-based, so this returns zero values.
	// For actual metrics, query Prometheus directly via /metrics endpoint.
	return &MetricsSnapshot{
		VectorsInserted:   0,
		VectorsDeleted:    0,
		Searches:          0,
		ActiveConnections: 0,
		VectorCount:       0,
		MemoryUsage:       0,
		CacheHitRate:      0,
	}
}
