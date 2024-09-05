package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

type RedisAdapter struct {
	client       *redis.Client
	clusterMode  bool
	logger       *zap.Logger
	connPool     *ConnectionPool
	shardManager ShardManagerInterface
}

// ShardManagerInterface defines the interface for shard management
// This is a local interface that matches the actual ShardManager implementation
type ShardManagerInterface interface {
	GetShardForKey(key string) (int, error)
	GetShardAdapter(shardID int) (*RedisAdapter, error)
}

// ShardInfo is now imported from internal/shard package
// Use shard.ShardInfo instead

type ConnectionPool struct {
	clients map[string]*redis.Client
	mu      sync.RWMutex
}

func NewConnectionPool() *ConnectionPool {
	return &ConnectionPool{
		clients: make(map[string]*redis.Client),
	}
}

func (cp *ConnectionPool) GetClient(addr string, password string, db int) (*redis.Client, error) {
	cp.mu.RLock()
	client, exists := cp.clients[addr]
	cp.mu.RUnlock()

	if exists {
		return client, nil
	}

	cp.mu.Lock()
	defer cp.mu.Unlock()

	client, exists = cp.clients[addr]
	if exists {
		return client, nil
	}

	client = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     100,
		MinIdleConns: 10,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	cp.clients[addr] = client
	return client, nil
}

func NewRedisAdapter(addr, password string, db int, clusterMode bool, logger *zap.Logger) (*RedisAdapter, error) {
	connPool := NewConnectionPool()
	client, err := connPool.GetClient(addr, password, db)
	if err != nil {
		return nil, err
	}

	return &RedisAdapter{
		client:      client,
		clusterMode: clusterMode,
		logger:      logger,
		connPool:    connPool,
	}, nil
}

func NewShardedRedisAdapter(shardManager ShardManagerInterface, logger *zap.Logger) *RedisAdapter {
	return &RedisAdapter{
		logger:       logger,
		shardManager: shardManager,
		connPool:     NewConnectionPool(),
	}
}

func (r *RedisAdapter) StoreVector(ctx context.Context, indexName string, vector *StoredVector) error {
	if r.shardManager != nil {
		return r.storeVectorSharded(ctx, indexName, vector)
	}

	key := fmt.Sprintf("vector:%s:%s", indexName, vector.ID)

	data, err := vector.ToBytes()
	if err != nil {
		return fmt.Errorf("failed to serialize vector: %w", err)
	}

	pipe := r.client.TxPipeline()
	pipe.Set(ctx, key, data, 0)
	pipe.ZAdd(ctx, fmt.Sprintf("index:%s:ids", indexName), &redis.Z{
		Score:  float64(vector.Timestamp),
		Member: vector.ID,
	})

	if len(vector.Metadata) > 0 {
		metaKey := fmt.Sprintf("metadata:%s:%s", indexName, vector.ID)
		metaData, _ := json.Marshal(vector.Metadata)
		pipe.Set(ctx, metaKey, metaData, 0)
	}

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) storeVectorSharded(ctx context.Context, indexName string, vector *StoredVector) error {
	shardKey := fmt.Sprintf("%s:%s", indexName, vector.ID)
	shardID, err := r.shardManager.GetShardForKey(shardKey)
	if err != nil {
		return fmt.Errorf("failed to get shard for key %s: %w", shardKey, err)
	}

	adapter, err := r.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return fmt.Errorf("failed to get adapter for shard %d: %w", shardID, err)
	}

	return adapter.StoreVector(ctx, indexName, vector)
}

func (r *RedisAdapter) BatchStoreVectors(ctx context.Context, indexName string, vectors []*StoredVector) error {
	if r.shardManager != nil {
		return r.batchStoreVectorsSharded(ctx, indexName, vectors)
	}

	pipe := r.client.TxPipeline()

	for _, vector := range vectors {
		key := fmt.Sprintf("vector:%s:%s", indexName, vector.ID)
		data, err := vector.ToBytes()
		if err != nil {
			r.logger.Error("Failed to serialize vector", zap.String("id", vector.ID), zap.Error(err))
			continue
		}

		pipe.Set(ctx, key, data, 0)
		pipe.ZAdd(ctx, fmt.Sprintf("index:%s:ids", indexName), &redis.Z{
			Score:  float64(vector.Timestamp),
			Member: vector.ID,
		})

		if len(vector.Metadata) > 0 {
			metaKey := fmt.Sprintf("metadata:%s:%s", indexName, vector.ID)
			metaData, _ := json.Marshal(vector.Metadata)
			pipe.Set(ctx, metaKey, metaData, 0)
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) batchStoreVectorsSharded(ctx context.Context, indexName string, vectors []*StoredVector) error {
	shardToVectors := make(map[int][]*StoredVector)

	for _, vector := range vectors {
		shardKey := fmt.Sprintf("%s:%s", indexName, vector.ID)
		shardID, err := r.shardManager.GetShardForKey(shardKey)
		if err != nil {
			r.logger.Error("Failed to get shard for vector", zap.String("id", vector.ID), zap.Error(err))
			continue
		}

		shardToVectors[shardID] = append(shardToVectors[shardID], vector)
	}

	for shardID, shardVectors := range shardToVectors {
		adapter, err := r.shardManager.GetShardAdapter(shardID)
		if err != nil {
			r.logger.Error("Failed to get adapter for shard", zap.Int("shard", shardID), zap.Error(err))
			continue
		}

		if err := adapter.BatchStoreVectors(ctx, indexName, shardVectors); err != nil {
			r.logger.Error("Failed to batch store vectors in shard", zap.Int("shard", shardID), zap.Error(err))
		}
	}

	return nil
}

func (r *RedisAdapter) GetVector(ctx context.Context, indexName, vectorID string) (*StoredVector, error) {
	if r.shardManager != nil {
		return r.getVectorSharded(ctx, indexName, vectorID)
	}

	key := fmt.Sprintf("vector:%s:%s", indexName, vectorID)

	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("vector not found: %s", vectorID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get vector: %w", err)
	}

	return VectorFromBytes(data)
}

func (r *RedisAdapter) getVectorSharded(ctx context.Context, indexName, vectorID string) (*StoredVector, error) {
	shardKey := fmt.Sprintf("%s:%s", indexName, vectorID)
	shardID, err := r.shardManager.GetShardForKey(shardKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard for key %s: %w", shardKey, err)
	}

	adapter, err := r.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return nil, fmt.Errorf("failed to get adapter for shard %d: %w", shardID, err)
	}

	return adapter.GetVector(ctx, indexName, vectorID)
}

func (r *RedisAdapter) DeleteVector(ctx context.Context, indexName, vectorID string) error {
	pipe := r.client.TxPipeline()

	pipe.Del(ctx, fmt.Sprintf("vector:%s:%s", indexName, vectorID))
	pipe.Del(ctx, fmt.Sprintf("metadata:%s:%s", indexName, vectorID))
	pipe.ZRem(ctx, fmt.Sprintf("index:%s:ids", indexName), vectorID)

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) BatchDeleteVectors(ctx context.Context, indexName string, vectorIDs []string) (int, error) {
	pipe := r.client.TxPipeline()

	for _, id := range vectorIDs {
		pipe.Del(ctx, fmt.Sprintf("vector:%s:%s", indexName, id))
		pipe.Del(ctx, fmt.Sprintf("metadata:%s:%s", indexName, id))
		pipe.ZRem(ctx, fmt.Sprintf("index:%s:ids", indexName), id)
	}

	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for i := 0; i < len(cmds); i += 3 {
		if cmds[i].Err() == nil {
			deleted++
		}
	}

	return deleted, nil
}

func (r *RedisAdapter) GetAllVectorIDs(ctx context.Context, indexName string) ([]string, error) {
	key := fmt.Sprintf("index:%s:ids", indexName)

	result, err := r.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (r *RedisAdapter) GetVectorCount(ctx context.Context, indexName string) (int64, error) {
	key := fmt.Sprintf("index:%s:ids", indexName)
	return r.client.ZCard(ctx, key).Result()
}

func (r *RedisAdapter) CreateIndex(ctx context.Context, index *VectorIndex) error {
	key := fmt.Sprintf("index:meta:%s", index.Name)

	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	return r.client.Set(ctx, key, data, 0).Err()
}

func (r *RedisAdapter) GetIndex(ctx context.Context, indexName string) (*VectorIndex, error) {
	key := fmt.Sprintf("index:meta:%s", indexName)

	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("index not found: %s", indexName)
	}
	if err != nil {
		return nil, err
	}

	var index VectorIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

func (r *RedisAdapter) DropIndex(ctx context.Context, indexName string) error {
	vectorIDs, err := r.GetAllVectorIDs(ctx, indexName)
	if err != nil {
		return err
	}

	pipe := r.client.TxPipeline()

	for _, id := range vectorIDs {
		pipe.Del(ctx, fmt.Sprintf("vector:%s:%s", indexName, id))
		pipe.Del(ctx, fmt.Sprintf("metadata:%s:%s", indexName, id))
	}

	pipe.Del(ctx, fmt.Sprintf("index:%s:ids", indexName))
	pipe.Del(ctx, fmt.Sprintf("index:meta:%s", indexName))

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) SearchVectors(ctx context.Context, indexName string, queryVector []float32, topK int, filter map[string]string) ([]*SearchResult, error) {
	if r.shardManager != nil {
		return r.searchVectorsSharded(ctx, indexName, queryVector, topK, filter)
	}

	vectorIDs, err := r.GetAllVectorIDs(ctx, indexName)
	if err != nil {
		return nil, err
	}

	index, err := r.GetIndex(ctx, indexName)
	if err != nil {
		return nil, err
	}

	distFunc := GetDistanceFunc(index.Metric)
	results := make([]*SearchResult, 0, len(vectorIDs))

	for _, id := range vectorIDs {
		vector, err := r.GetVector(ctx, indexName, id)
		if err != nil {
			r.logger.Warn("Failed to get vector during search", zap.String("id", id), zap.Error(err))
			continue
		}

		if len(filter) > 0 {
			match := true
			for k, v := range filter {
				if vector.Metadata[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		distance := distFunc(queryVector, vector.Values)
		score := 1.0 / (1.0 + distance)

		results = append(results, &SearchResult{
			ID:       id,
			Score:    score,
			Distance: distance,
			Vector:   vector,
		})
	}

	if len(results) > topK {
		quickSelect(results, topK)
		results = results[:topK]
	}

	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Score < results[j].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results, nil
}

func (r *RedisAdapter) searchVectorsSharded(ctx context.Context, indexName string, queryVector []float32, topK int, filter map[string]string) ([]*SearchResult, error) {
	// Search across all shards (shardManager handles health internally)
	allResults := make([]*SearchResult, 0)

	// Note: In a real implementation, we'd iterate through known shards
	// For now, this is handled by the upper layer
	adapter, err := r.shardManager.GetShardAdapter(0)
	if err != nil {
		if err != nil {
			r.logger.Warn("Failed to get adapter for shard", zap.Int("shard", shard.ID), zap.Error(err))
			continue
		}

		shardResults, err := adapter.SearchVectors(ctx, indexName, queryVector, topK*2, filter)
		if err != nil {
			r.logger.Warn("Failed to search in shard", zap.Int("shard", shard.ID), zap.Error(err))
			continue
		}

		allResults = append(allResults, shardResults...)
	}

	if len(allResults) > topK {
		quickSelect(allResults, topK)
		allResults = allResults[:topK]
	}

	for i := 0; i < len(allResults)-1; i++ {
		for j := i + 1; j < len(allResults); j++ {
			if allResults[i].Score < allResults[j].Score {
				allResults[i], allResults[j] = allResults[j], allResults[i]
			}
		}
	}

	return allResults, nil
}

func quickSelect(results []*SearchResult, k int) {
	left, right := 0, len(results)-1
	for left < right {
		pivotIdx := partition(results, left, right)
		if pivotIdx == k {
			return
		} else if pivotIdx < k {
			left = pivotIdx + 1
		} else {
			right = pivotIdx - 1
		}
	}
}

func partition(results []*SearchResult, left, right int) int {
	pivot := results[right].Score
	i := left
	for j := left; j < right; j++ {
		if results[j].Score > pivot {
			results[i], results[j] = results[j], results[i]
			i++
		}
	}
	results[i], results[right] = results[right], results[i]
	return i
}

func (r *RedisAdapter) Close() error {
	return r.client.Close()
}
