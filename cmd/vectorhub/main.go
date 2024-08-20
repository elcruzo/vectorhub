package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/elcruzo/vectorhub/api/proto"
	"github.com/elcruzo/vectorhub/internal/config"
	"github.com/elcruzo/vectorhub/internal/metrics"
	"github.com/elcruzo/vectorhub/internal/replication"
	"github.com/elcruzo/vectorhub/internal/server"
	"github.com/elcruzo/vectorhub/internal/shard"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

var (
	configFile  = flag.String("config", "configs/config.yaml", "Path to configuration file")
	port        = flag.Int("port", 50051, "gRPC server port")
	metricsPort = flag.Int("metrics-port", 9090, "Metrics server port")
)

func main() {
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := config.Load(*configFile)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	metricsCollector := metrics.NewCollector()

	shardConfig := &shard.ShardConfig{
		ShardCount:          cfg.Sharding.ShardCount,
		ReplicaCount:        cfg.Sharding.ReplicaCount,
		VirtualNodes:        cfg.Sharding.VirtualNodes,
		HealthCheckInterval: time.Duration(cfg.Sharding.HealthCheckIntervalSeconds) * time.Second,
		RedisAddresses:      cfg.Redis.Addresses,
		RedisPassword:       cfg.Redis.Password,
		RedisDB:             cfg.Redis.DB,
	}

	shardManager := shard.NewShardManager(shardConfig, logger)

	replicationConfig := &replication.Config{
		ReplicationFactor: cfg.Replication.Factor,
		SyncInterval:      time.Duration(cfg.Replication.SyncIntervalSeconds) * time.Second,
		MaxLag:            time.Duration(cfg.Replication.MaxLagSeconds) * time.Second,
		FailoverTimeout:   time.Duration(cfg.Replication.FailoverTimeoutSeconds) * time.Second,
		RedisPassword:     cfg.Redis.Password,
		RedisDB:           cfg.Redis.DB,
	}

	replicationManager := replication.NewManager(replicationConfig, logger)

	for i := 0; i < cfg.Sharding.ShardCount; i++ {
		for j, addr := range cfg.Replication.ReplicaAddresses {
			role := "secondary"
			if j == 0 {
				role = "primary"
			}

			node := &replication.ReplicaNode{
				ID:       fmt.Sprintf("replica-%d-%d", i, j),
				Address:  addr,
				Role:     role,
				Status:   "active",
				LastSync: time.Now(),
			}

			if err := replicationManager.RegisterReplica(i, node); err != nil {
				logger.Warn("Failed to register replica",
					zap.Int("shard", i),
					zap.String("address", addr),
					zap.Error(err))
			}
		}
	}

	vectorService := server.NewVectorService(
		shardManager,
		replicationManager,
		metricsCollector,
		logger,
	)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		logger.Fatal("Failed to listen", zap.Error(err))
	}

	kaep := keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second,
		PermitWithoutStream: true,
	}

	kasp := keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Second,
		MaxConnectionAge:      30 * time.Second,
		MaxConnectionAgeGrace: 5 * time.Second,
		Time:                  5 * time.Second,
		Timeout:               1 * time.Second,
	}

	serverOpts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(kaep),
		grpc.KeepaliveParams(kasp),
		grpc.MaxRecvMsgSize(100 * 1024 * 1024),
		grpc.MaxSendMsgSize(100 * 1024 * 1024),
		grpc.MaxConcurrentStreams(1000),
	}

	// Add TLS credentials if enabled
	if cfg.Server.TLSEnabled {
		if cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "" {
			logger.Fatal("TLS enabled but certificate files not provided")
		}

		cert, err := tls.LoadX509KeyPair(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		if err != nil {
			logger.Fatal("Failed to load TLS certificates", zap.Error(err))
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			},
		}

		creds := credentials.NewTLS(tlsConfig)
		serverOpts = append(serverOpts, grpc.Creds(creds))

		logger.Info("TLS enabled for gRPC server",
			zap.String("cert", cfg.Server.TLSCertFile),
			zap.String("key", cfg.Server.TLSKeyFile))
	} else {
		logger.Warn("TLS is DISABLED - not recommended for production")
	}

	grpcServer := grpc.NewServer(serverOpts...)

	pb.RegisterVectorServiceServer(grpcServer, vectorService)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("vectorhub.api.v1.VectorService", grpc_health_v1.HealthCheckResponse_SERVING)

	reflection.Register(grpcServer)

	healthChecker := server.NewHealthChecker(shardManager, replicationManager, logger)

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/health", healthChecker.HTTPHandler())
		http.HandleFunc("/health/live", healthChecker.LivenessHandler())
		http.HandleFunc("/health/ready", healthChecker.ReadinessHandler())

		logger.Info("Starting metrics and health server", zap.Int("port", *metricsPort))
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *metricsPort), nil); err != nil {
			logger.Error("Failed to start metrics server", zap.Error(err))
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("Starting gRPC server", zap.Int("port", *port))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Failed to serve", zap.Error(err))
		}
	}()

	<-sigCh
	logger.Info("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	go func() {
		grpcServer.GracefulStop()
	}()

	<-shutdownCtx.Done()

	if err := shardManager.Close(); err != nil {
		logger.Error("Failed to close shard manager", zap.Error(err))
	}

	if err := replicationManager.Close(); err != nil {
		logger.Error("Failed to close replication manager", zap.Error(err))
	}

	logger.Info("Server shutdown complete")
}
