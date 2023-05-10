package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Server       ServerConfig       `mapstructure:"server"`
	Redis        RedisConfig        `mapstructure:"redis"`
	Sharding     ShardingConfig     `mapstructure:"sharding"`
	Replication  ReplicationConfig  `mapstructure:"replication"`
	Metrics      MetricsConfig      `mapstructure:"metrics"`
	Logging      LoggingConfig      `mapstructure:"logging"`
	Performance  PerformanceConfig  `mapstructure:"performance"`
}

type ServerConfig struct {
	GRPCPort              int    `mapstructure:"grpc_port"`
	MetricsPort           int    `mapstructure:"metrics_port"`
	MaxMessageSizeMB      int    `mapstructure:"max_message_size_mb"`
	MaxConcurrentStreams  int    `mapstructure:"max_concurrent_streams"`
	ConnectionTimeout     int    `mapstructure:"connection_timeout_seconds"`
	KeepAliveTime         int    `mapstructure:"keepalive_time_seconds"`
	KeepAliveTimeout      int    `mapstructure:"keepalive_timeout_seconds"`
	TLSEnabled            bool   `mapstructure:"tls_enabled"`
	TLSCertFile           string `mapstructure:"tls_cert_file"`
	TLSKeyFile            string `mapstructure:"tls_key_file"`
}

type RedisConfig struct {
	Addresses         []string `mapstructure:"addresses"`
	Password          string   `mapstructure:"password"`
	DB                int      `mapstructure:"db"`
	PoolSize          int      `mapstructure:"pool_size"`
	MinIdleConns      int      `mapstructure:"min_idle_conns"`
	MaxRetries        int      `mapstructure:"max_retries"`
	DialTimeout       int      `mapstructure:"dial_timeout_seconds"`
	ReadTimeout       int      `mapstructure:"read_timeout_seconds"`
	WriteTimeout      int      `mapstructure:"write_timeout_seconds"`
	ClusterMode       bool     `mapstructure:"cluster_mode"`
	SentinelMode      bool     `mapstructure:"sentinel_mode"`
	SentinelMasterName string  `mapstructure:"sentinel_master_name"`
}

type ShardingConfig struct {
	ShardCount                 int `mapstructure:"shard_count"`
	ReplicaCount               int `mapstructure:"replica_count"`
	VirtualNodes               int `mapstructure:"virtual_nodes"`
	HealthCheckIntervalSeconds int `mapstructure:"health_check_interval_seconds"`
	RebalanceEnabled           bool `mapstructure:"rebalance_enabled"`
	RebalanceIntervalMinutes   int `mapstructure:"rebalance_interval_minutes"`
}

type ReplicationConfig struct {
	Factor                  int      `mapstructure:"factor"`
	SyncIntervalSeconds     int      `mapstructure:"sync_interval_seconds"`
	MaxLagSeconds           int      `mapstructure:"max_lag_seconds"`
	FailoverTimeoutSeconds  int      `mapstructure:"failover_timeout_seconds"`
	ReplicaAddresses        []string `mapstructure:"replica_addresses"`
	AsyncReplication        bool     `mapstructure:"async_replication"`
	CompressionEnabled      bool     `mapstructure:"compression_enabled"`
}

type MetricsConfig struct {
	Enabled              bool     `mapstructure:"enabled"`
	Namespace            string   `mapstructure:"namespace"`
	Subsystem            string   `mapstructure:"subsystem"`
	HistogramBuckets     []float64 `mapstructure:"histogram_buckets"`
	CollectionInterval   int      `mapstructure:"collection_interval_seconds"`
	EnableDetailedMetrics bool     `mapstructure:"enable_detailed_metrics"`
}

type LoggingConfig struct {
	Level             string `mapstructure:"level"`
	Format            string `mapstructure:"format"`
	OutputPath        string `mapstructure:"output_path"`
	ErrorOutputPath   string `mapstructure:"error_output_path"`
	EnableStacktrace  bool   `mapstructure:"enable_stacktrace"`
	EnableCaller      bool   `mapstructure:"enable_caller"`
	EnableSampling    bool   `mapstructure:"enable_sampling"`
	SamplingInitial   int    `mapstructure:"sampling_initial"`
	SamplingThereafter int   `mapstructure:"sampling_thereafter"`
}

type PerformanceConfig struct {
	MaxCPU              int  `mapstructure:"max_cpu"`
	MaxMemoryGB         int  `mapstructure:"max_memory_gb"`
	EnableProfiling     bool `mapstructure:"enable_profiling"`
	ProfilingPort       int  `mapstructure:"profiling_port"`
	GCPercent           int  `mapstructure:"gc_percent"`
	EnableMemoryLimit   bool `mapstructure:"enable_memory_limit"`
	VectorCacheSize     int  `mapstructure:"vector_cache_size_mb"`
	IndexCacheSize      int  `mapstructure:"index_cache_size_mb"`
	QueryCacheEnabled   bool `mapstructure:"query_cache_enabled"`
	QueryCacheTTL       int  `mapstructure:"query_cache_ttl_seconds"`
}

func Load(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return defaultConfig(), nil
	}

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetEnvPrefix("VECTORHUB")
	viper.AutomaticEnv()

	viper.SetDefault("server.grpc_port", 50051)
	viper.SetDefault("server.metrics_port", 9090)
	viper.SetDefault("server.max_message_size_mb", 100)
	viper.SetDefault("server.max_concurrent_streams", 1000)
	viper.SetDefault("server.connection_timeout_seconds", 30)
	viper.SetDefault("server.keepalive_time_seconds", 10)
	viper.SetDefault("server.keepalive_timeout_seconds", 5)

	viper.SetDefault("redis.addresses", []string{"localhost:6379"})
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 100)
	viper.SetDefault("redis.min_idle_conns", 10)
	viper.SetDefault("redis.max_retries", 3)
	viper.SetDefault("redis.dial_timeout_seconds", 5)
	viper.SetDefault("redis.read_timeout_seconds", 3)
	viper.SetDefault("redis.write_timeout_seconds", 3)

	viper.SetDefault("sharding.shard_count", 8)
	viper.SetDefault("sharding.replica_count", 2)
	viper.SetDefault("sharding.virtual_nodes", 150)
	viper.SetDefault("sharding.health_check_interval_seconds", 10)

	viper.SetDefault("replication.factor", 2)
	viper.SetDefault("replication.sync_interval_seconds", 5)
	viper.SetDefault("replication.max_lag_seconds", 30)
	viper.SetDefault("replication.failover_timeout_seconds", 60)

	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.namespace", "vectorhub")
	viper.SetDefault("metrics.subsystem", "server")
	viper.SetDefault("metrics.collection_interval_seconds", 10)

	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")

	viper.SetDefault("performance.gc_percent", 100)
	viper.SetDefault("performance.vector_cache_size_mb", 1024)
	viper.SetDefault("performance.index_cache_size_mb", 512)
	viper.SetDefault("performance.query_cache_ttl_seconds", 60)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &config, nil
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCPort:             50051,
			MetricsPort:          9090,
			MaxMessageSizeMB:     100,
			MaxConcurrentStreams: 1000,
			ConnectionTimeout:    30,
			KeepAliveTime:        10,
			KeepAliveTimeout:     5,
		},
		Redis: RedisConfig{
			Addresses:    []string{"localhost:6379"},
			DB:           0,
			PoolSize:     100,
			MinIdleConns: 10,
			MaxRetries:   3,
			DialTimeout:  5,
			ReadTimeout:  3,
			WriteTimeout: 3,
		},
		Sharding: ShardingConfig{
			ShardCount:                 8,
			ReplicaCount:               2,
			VirtualNodes:               150,
			HealthCheckIntervalSeconds: 10,
		},
		Replication: ReplicationConfig{
			Factor:                 2,
			SyncIntervalSeconds:    5,
			MaxLagSeconds:          30,
			FailoverTimeoutSeconds: 60,
			ReplicaAddresses:       []string{"localhost:6380", "localhost:6381"},
		},
		Metrics: MetricsConfig{
			Enabled:                 true,
			Namespace:               "vectorhub",
			Subsystem:               "server",
			CollectionInterval:      10,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Performance: PerformanceConfig{
			GCPercent:           100,
			VectorCacheSize:     1024,
			IndexCacheSize:      512,
			QueryCacheTTL:       60,
		},
	}
}

func (c *Config) Validate() error {
	if c.Server.GRPCPort <= 0 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid gRPC port: %d", c.Server.GRPCPort)
	}

	if c.Server.MetricsPort <= 0 || c.Server.MetricsPort > 65535 {
		return fmt.Errorf("invalid metrics port: %d", c.Server.MetricsPort)
	}

	if len(c.Redis.Addresses) == 0 {
		return fmt.Errorf("at least one Redis address is required")
	}

	if c.Sharding.ShardCount <= 0 {
		return fmt.Errorf("shard count must be positive")
	}

	if c.Sharding.VirtualNodes <= 0 {
		return fmt.Errorf("virtual nodes count must be positive")
	}

	return nil
}