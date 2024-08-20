package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/elcruzo/vectorhub/internal/replication"
	"github.com/elcruzo/vectorhub/internal/shard"
	"go.uber.org/zap"
)

type HealthChecker struct {
	shardManager       ShardManagerInterface
	replicationManager ReplicationManagerInterface
	logger             *zap.Logger
}

type ShardManagerInterface interface {
	GetAllShards() []*shard.ShardInfo
	GetHealthyShards() []*shard.ShardInfo
}

type ReplicationManagerInterface interface {
	GetReplicaStatus(shardID int) ([]*replication.ReplicaNode, error)
}

type HealthResponse struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Version     string            `json:"version"`
	Shards      []ShardHealth     `json:"shards"`
	Replication ReplicationHealth `json:"replication"`
	Overall     OverallHealth     `json:"overall"`
}

type ShardHealth struct {
	ID          int       `json:"id"`
	Node        string    `json:"node"`
	Status      string    `json:"status"`
	VectorCount int64     `json:"vector_count"`
	LastCheck   time.Time `json:"last_check"`
	Healthy     bool      `json:"healthy"`
}

type ReplicationHealth struct {
	TotalReplicas  int     `json:"total_replicas"`
	ActiveReplicas int     `json:"active_replicas"`
	FailedReplicas int     `json:"failed_replicas"`
	AverageLag     float64 `json:"average_lag_ms"`
	Healthy        bool    `json:"healthy"`
}

type OverallHealth struct {
	Healthy          bool    `json:"healthy"`
	TotalShards      int     `json:"total_shards"`
	HealthyShards    int     `json:"healthy_shards"`
	HealthPercentage float64 `json:"health_percentage"`
	Message          string  `json:"message"`
}

func NewHealthChecker(
	shardManager ShardManagerInterface,
	replicationManager ReplicationManagerInterface,
	logger *zap.Logger,
) *HealthChecker {
	return &HealthChecker{
		shardManager:       shardManager,
		replicationManager: replicationManager,
		logger:             logger,
	}
}

func (h *HealthChecker) Check(ctx context.Context) *HealthResponse {
	allShards := h.shardManager.GetAllShards()
	healthyShards := h.shardManager.GetHealthyShards()

	shardHealths := make([]ShardHealth, 0, len(allShards))
	for _, shardInfo := range allShards {
		shardHealths = append(shardHealths, ShardHealth{
			ID:          shardInfo.ID,
			Node:        shardInfo.Node,
			Status:      shardInfo.Status,
			VectorCount: shardInfo.VectorCount,
			LastCheck:   shardInfo.LastHealthCheck,
			Healthy:     shardInfo.Status == "active",
		})
	}

	// Check replication health
	totalReplicas := 0
	activeReplicas := 0
	failedReplicas := 0
	totalLag := int64(0)

	for _, shardInfo := range allShards {
		replicas, err := h.replicationManager.GetReplicaStatus(shardInfo.ID)
		if err != nil {
			h.logger.Warn("Failed to get replica status", zap.Int("shard", shardInfo.ID), zap.Error(err))
			continue
		}

		for _, replica := range replicas {
			totalReplicas++
			if replica.Status == "active" {
				activeReplicas++
			} else {
				failedReplicas++
			}
			totalLag += int64(replica.Lag.Milliseconds())
		}
	}

	averageLag := float64(0)
	if totalReplicas > 0 {
		averageLag = float64(totalLag) / float64(totalReplicas)
	}

	replicationHealth := ReplicationHealth{
		TotalReplicas:  totalReplicas,
		ActiveReplicas: activeReplicas,
		FailedReplicas: failedReplicas,
		AverageLag:     averageLag,
		Healthy:        failedReplicas == 0 && averageLag < 1000, // Less than 1 second lag
	}

	// Calculate overall health
	totalShards := len(allShards)
	healthyShardsCount := len(healthyShards)
	healthPercentage := float64(0)
	if totalShards > 0 {
		healthPercentage = (float64(healthyShardsCount) / float64(totalShards)) * 100
	}

	overallHealthy := healthPercentage >= 50 && replicationHealth.Healthy
	status := "healthy"
	message := "All systems operational"

	if !overallHealthy {
		status = "unhealthy"
		if healthPercentage < 50 {
			message = "Less than 50% of shards are healthy"
		} else if !replicationHealth.Healthy {
			message = "Replication issues detected"
		}
	} else if healthPercentage < 100 {
		status = "degraded"
		message = "Some shards are unhealthy but system is operational"
	}

	return &HealthResponse{
		Status:      status,
		Timestamp:   time.Now(),
		Version:     "1.0.0",
		Shards:      shardHealths,
		Replication: replicationHealth,
		Overall: OverallHealth{
			Healthy:          overallHealthy,
			TotalShards:      totalShards,
			HealthyShards:    healthyShardsCount,
			HealthPercentage: healthPercentage,
			Message:          message,
		},
	}
}

func (h *HealthChecker) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		health := h.Check(ctx)

		w.Header().Set("Content-Type", "application/json")

		statusCode := http.StatusOK
		if health.Status == "unhealthy" {
			statusCode = http.StatusServiceUnavailable
		} else if health.Status == "degraded" {
			statusCode = http.StatusOK // Still return 200 for degraded
		}

		w.WriteHeader(statusCode)

		if err := json.NewEncoder(w).Encode(health); err != nil {
			h.logger.Error("Failed to encode health response", zap.Error(err))
		}
	}
}

func (h *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Liveness probe - just check if the server is running
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}
}

func (h *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		health := h.Check(ctx)

		// Readiness probe - check if we can serve traffic
		if health.Overall.Healthy {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Not Ready"))
		}
	}
}
