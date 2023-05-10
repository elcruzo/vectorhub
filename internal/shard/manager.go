package shard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/elcruzo/vectorhub/internal/storage"
	"go.uber.org/zap"
)

type ShardInfo struct {
	ID           int
	Node         string
	Status       string
	VectorCount  int64
	MemoryUsage  int64
	LastHealthCheck time.Time
	Replicas     []string
}

type ShardManager struct {
	consistentHash *ConsistentHash
	shards         map[int]*ShardInfo
	nodeToShards   map[string][]int
	shardToAdapter map[int]*storage.RedisAdapter
	config         *ShardConfig
	logger         *zap.Logger
	mu             sync.RWMutex
	healthTicker   *time.Ticker
	ctx            context.Context
	cancel         context.CancelFunc
}

type ShardConfig struct {
	ShardCount      int
	ReplicaCount    int
	VirtualNodes    int
	HealthCheckInterval time.Duration
	RedisAddresses  []string
	RedisPassword   string
	RedisDB         int
}

func NewShardManager(config *ShardConfig, logger *zap.Logger) *ShardManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	sm := &ShardManager{
		consistentHash: NewConsistentHash(config.VirtualNodes),
		shards:         make(map[int]*ShardInfo),
		nodeToShards:   make(map[string][]int),
		shardToAdapter: make(map[int]*storage.RedisAdapter),
		config:         config,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
	}

	for i := 0; i < config.ShardCount; i++ {
		nodeAddr := config.RedisAddresses[i%len(config.RedisAddresses)]
		sm.consistentHash.AddNode(nodeAddr)
		
		shard := &ShardInfo{
			ID:     i,
			Node:   nodeAddr,
			Status: "active",
			LastHealthCheck: time.Now(),
		}
		
		sm.shards[i] = shard
		
		if _, exists := sm.nodeToShards[nodeAddr]; !exists {
			sm.nodeToShards[nodeAddr] = make([]int, 0)
		}
		sm.nodeToShards[nodeAddr] = append(sm.nodeToShards[nodeAddr], i)
		
		adapter, err := storage.NewRedisAdapter(storage.RedisConfig{
			Addr:     nodeAddr,
			Password: config.RedisPassword,
			DB:       config.RedisDB + i,
		}, logger)
		
		if err != nil {
			logger.Error("Failed to create Redis adapter for shard", 
				zap.Int("shard", i),
				zap.String("node", nodeAddr),
				zap.Error(err))
			shard.Status = "error"
		} else {
			sm.shardToAdapter[i] = adapter
		}
	}

	sm.startHealthCheck()
	
	return sm
}

func (sm *ShardManager) GetShardForKey(key string) (int, error) {
	node, err := sm.consistentHash.GetNode(key)
	if err != nil {
		return -1, err
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	shards, exists := sm.nodeToShards[node]
	if !exists || len(shards) == 0 {
		return -1, fmt.Errorf("no shards found for node: %s", node)
	}

	hash := crc32Hash(key)
	shardIndex := hash % uint32(len(shards))
	
	return shards[shardIndex], nil
}

func (sm *ShardManager) GetShardAdapter(shardID int) (*storage.RedisAdapter, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	adapter, exists := sm.shardToAdapter[shardID]
	if !exists {
		return nil, fmt.Errorf("no adapter found for shard: %d", shardID)
	}

	shard, exists := sm.shards[shardID]
	if !exists || shard.Status != "active" {
		return nil, fmt.Errorf("shard %d is not active", shardID)
	}

	return adapter, nil
}

func (sm *ShardManager) GetAllShards() []*ShardInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	shards := make([]*ShardInfo, 0, len(sm.shards))
	for _, shard := range sm.shards {
		shardCopy := *shard
		shards = append(shards, &shardCopy)
	}

	return shards
}

func (sm *ShardManager) GetHealthyShards() []*ShardInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	shards := make([]*ShardInfo, 0)
	for _, shard := range sm.shards {
		if shard.Status == "active" {
			shardCopy := *shard
			shards = append(shards, &shardCopy)
		}
	}

	return shards
}

func (sm *ShardManager) startHealthCheck() {
	sm.healthTicker = time.NewTicker(sm.config.HealthCheckInterval)
	
	go func() {
		for {
			select {
			case <-sm.healthTicker.C:
				sm.performHealthCheck()
			case <-sm.ctx.Done():
				return
			}
		}
	}()
}

func (sm *ShardManager) performHealthCheck() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for shardID, shard := range sm.shards {
		adapter, exists := sm.shardToAdapter[shardID]
		if !exists {
			shard.Status = "no_adapter"
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		
		count, err := adapter.GetVectorCount(ctx, "_health_check")
		cancel()

		if err != nil {
			sm.logger.Warn("Health check failed for shard",
				zap.Int("shard", shardID),
				zap.String("node", shard.Node),
				zap.Error(err))
			shard.Status = "unhealthy"
		} else {
			shard.Status = "active"
			shard.VectorCount = count
		}

		shard.LastHealthCheck = time.Now()
	}
}

func (sm *ShardManager) RebalanceShards() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	healthyShards := 0
	for _, shard := range sm.shards {
		if shard.Status == "active" {
			healthyShards++
		}
	}

	if healthyShards < sm.config.ShardCount/2 {
		return fmt.Errorf("insufficient healthy shards for rebalancing: %d/%d", healthyShards, sm.config.ShardCount)
	}

	sm.logger.Info("Starting shard rebalancing", zap.Int("healthy_shards", healthyShards))

	return nil
}

func (sm *ShardManager) GetShardStats(shardID int) (*ShardInfo, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	shard, exists := sm.shards[shardID]
	if !exists {
		return nil, fmt.Errorf("shard not found: %d", shardID)
	}

	shardCopy := *shard
	return &shardCopy, nil
}

func (sm *ShardManager) UpdateShardReplicas(shardID int, replicas []string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	shard, exists := sm.shards[shardID]
	if !exists {
		return fmt.Errorf("shard not found: %d", shardID)
	}

	shard.Replicas = replicas
	return nil
}

func (sm *ShardManager) Close() error {
	sm.cancel()
	
	if sm.healthTicker != nil {
		sm.healthTicker.Stop()
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, adapter := range sm.shardToAdapter {
		if err := adapter.Close(); err != nil {
			sm.logger.Error("Failed to close adapter", zap.Error(err))
		}
	}

	return nil
}

func crc32Hash(key string) uint32 {
	h := uint32(0)
	for i := 0; i < len(key); i++ {
		h = h*31 + uint32(key[i])
	}
	return h
}