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
type ShardManagerInterface interface {
	GetShardForKey(key string) (int, error)
	GetShardAdapter(shardID int) (*RedisAdapter, error)
}

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
	if client, exists := cp.clients[addr]; exists {
		cp.mu.RUnlock()
		return client, nil
	}
	cp.mu.RUnlock()

	cp.mu.Lock()
	defer cp.mu.Unlock()

	if client, exists := cp.clients[addr]; exists {
		return client, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     100,
		MinIdleConns: 10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis at %s: %w", addr, err)
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
		shardManager: shardManager,
		logger:       logger,
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

	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, data, 0)
	pipe.ZAdd(ctx, fmt.Sprintf("index:%s:ids", indexName), &redis.Z{
		Score:  float64(vector.Timestamp),
		Member: vector.ID,
	})

	if len(vector.Metadata) > 0 {
		metaKey := fmt.Sprintf("metadata:%s:%s", indexName, vector.ID)
		if metaData, err := json.Marshal(vector.Metadata); err == nil {
			pipe.Set(ctx, metaKey, metaData, 0)
		}
	}

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) storeVectorSharded(ctx context.Context, indexName string, vector *StoredVector) error {
	shardKey := fmt.Sprintf("%s:%s", indexName, vector.ID)
	shardID, err := r.shardManager.GetShardForKey(shardKey)
	if err != nil {
		return err
	}

	adapter, err := r.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return err
	}

	return adapter.StoreVector(ctx, indexName, vector)
}

func (r *RedisAdapter) BatchStoreVectors(ctx context.Context, indexName string, vectors []*StoredVector) error {
	if r.shardManager != nil {
		return r.batchStoreVectorsSharded(ctx, indexName, vectors)
	}

	pipe := r.client.Pipeline()

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
			if metaData, err := json.Marshal(vector.Metadata); err == nil {
				pipe.Set(ctx, metaKey, metaData, 0)
			}
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
	} else if err != nil {
		return nil, fmt.Errorf("failed to get vector: %w", err)
	}

	return VectorFromBytes(data)
}

func (r *RedisAdapter) getVectorSharded(ctx context.Context, indexName, vectorID string) (*StoredVector, error) {
	shardKey := fmt.Sprintf("%s:%s", indexName, vectorID)
	shardID, err := r.shardManager.GetShardForKey(shardKey)
	if err != nil {
		return nil, err
	}

	adapter, err := r.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return nil, err
	}

	return adapter.GetVector(ctx, indexName, vectorID)
}

func (r *RedisAdapter) DeleteVector(ctx context.Context, indexName, vectorID string) error {
	pipe := r.client.Pipeline()

	pipe.Del(ctx, fmt.Sprintf("vector:%s:%s", indexName, vectorID))
	pipe.Del(ctx, fmt.Sprintf("metadata:%s:%s", indexName, vectorID))
	pipe.ZRem(ctx, fmt.Sprintf("index:%s:ids", indexName), vectorID)

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) BatchDeleteVectors(ctx context.Context, indexName string, vectorIDs []string) (int, error) {
	pipe := r.client.Pipeline()

	for _, id := range vectorIDs {
		pipe.Del(ctx, fmt.Sprintf("vector:%s:%s", indexName, id))
	}

	results, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, result := range results {
		if result.Err() == nil {
			deleted++
		}
	}

	return deleted, nil
}

func (r *RedisAdapter) GetAllVectorIDs(ctx context.Context, indexName string) ([]string, error) {
	ids, err := r.client.ZRevRange(ctx, fmt.Sprintf("index:%s:ids", indexName), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *RedisAdapter) GetVectorCount(ctx context.Context, indexName string) (int64, error) {
	return r.client.ZCard(ctx, fmt.Sprintf("index:%s:ids", indexName)).Result()
}

func (r *RedisAdapter) CreateIndex(ctx context.Context, index *VectorIndex) error {
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("index:%s:meta", index.Name)
	return r.client.Set(ctx, key, data, 0).Err()
}

func (r *RedisAdapter) DropIndex(ctx context.Context, indexName string) error {
	pipe := r.client.Pipeline()

	pipe.Del(ctx, fmt.Sprintf("index:%s:meta", indexName))
	pipe.Del(ctx, fmt.Sprintf("index:%s:ids", indexName))

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisAdapter) GetIndex(ctx context.Context, indexName string) (*VectorIndex, error) {
	key := fmt.Sprintf("index:%s:meta", indexName)
	data, err := r.client.Get(ctx, key).Bytes()

	if err == redis.Nil {
		return nil, fmt.Errorf("index not found: %s", indexName)
	} else if err != nil {
		return nil, err
	}

	var index VectorIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

func (r *RedisAdapter) SearchVectors(ctx context.Context, indexName string, queryVector []float32, topK int, filter map[string]string) ([]*SearchResult, error) {
	if r.shardManager != nil {
		return r.searchVectorsSharded(ctx, indexName, queryVector, topK, filter)
	}

	ids, err := r.GetAllVectorIDs(ctx, indexName)
	if err != nil {
		return nil, err
	}

	results := make([]*SearchResult, 0)
	distFunc := GetDistanceFunc("cosine")

	for _, id := range ids {
		vector, err := r.GetVector(ctx, indexName, id)
		if err != nil {
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

		score := distFunc(queryVector, vector.Values)
		results = append(results, &SearchResult{
			ID:     vector.ID,
			Score:  score,
			Vector: vector,
		})
	}

	if len(results) > topK {
		QuickSelectResults(results, topK)
		results = results[:topK]
	}

	SortResultsByScore(results)

	return results, nil
}

func (r *RedisAdapter) searchVectorsSharded(ctx context.Context, indexName string, queryVector []float32, topK int, filter map[string]string) ([]*SearchResult, error) {
	return r.SearchVectors(ctx, indexName, queryVector, topK, filter)
}

func (r *RedisAdapter) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
