package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/elcruzo/vectorhub/api/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.VectorServiceClient
	config *Config
	mu     sync.RWMutex
}

type Config struct {
	Address         string
	MaxRetries      int
	Timeout         time.Duration
	KeepAlive       time.Duration
	MaxMessageSize  int
	BackoffMaxDelay time.Duration
}

func NewClient(config *Config) (*Client, error) {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.KeepAlive == 0 {
		config.KeepAlive = 10 * time.Second
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 100 * 1024 * 1024 // 100MB
	}
	if config.BackoffMaxDelay == 0 {
		config.BackoffMaxDelay = 10 * time.Second
	}

	kacp := keepalive.ClientParameters{
		Time:                config.KeepAlive,
		Timeout:             config.KeepAlive,
		PermitWithoutStream: true,
	}

	bc := backoff.Config{
		BaseDelay:  1.0 * time.Second,
		Multiplier: 1.6,
		Jitter:     0.2,
		MaxDelay:   config.BackoffMaxDelay,
	}

	conn, err := grpc.Dial(config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(config.MaxMessageSize),
			grpc.MaxCallSendMsgSize(config.MaxMessageSize),
		),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           bc,
			MinConnectTimeout: 20 * time.Second,
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		client: pb.NewVectorServiceClient(conn),
		config: config,
	}, nil
}

func (c *Client) Insert(ctx context.Context, indexName string, id string, values []float32, metadata map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.InsertRequest{
		IndexName: indexName,
		Vector: &pb.Vector{
			Id:       id,
			Values:   values,
			Metadata: metadata,
		},
	}

	resp, err := c.client.Insert(ctx, req)
	if err != nil {
		return fmt.Errorf("insert failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("insert failed: %s", resp.Message)
	}

	return nil
}

func (c *Client) BatchInsert(ctx context.Context, indexName string, vectors []Vector, parallel bool) (*BatchInsertResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	pbVectors := make([]*pb.Vector, len(vectors))
	for i, v := range vectors {
		pbVectors[i] = &pb.Vector{
			Id:       v.ID,
			Values:   v.Values,
			Metadata: v.Metadata,
		}
	}

	req := &pb.BatchInsertRequest{
		IndexName: indexName,
		Vectors:   pbVectors,
		Parallel:  parallel,
	}

	resp, err := c.client.BatchInsert(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("batch insert failed: %w", err)
	}

	return &BatchInsertResult{
		Success:       resp.Success,
		InsertedCount: int(resp.InsertedCount),
		FailedIDs:     resp.FailedIds,
	}, nil
}

func (c *Client) Search(ctx context.Context, indexName string, queryVector []float32, options SearchOptions) ([]*SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.SearchRequest{
		IndexName:       indexName,
		QueryVector:     queryVector,
		TopK:            int32(options.TopK),
		Filter:          options.Filter,
		MinScore:        options.MinScore,
		IncludeMetadata: options.IncludeMetadata,
	}

	resp, err := c.client.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("search failed: %s", resp.Message)
	}

	results := make([]*SearchResult, len(resp.Results))
	for i, r := range resp.Results {
		result := &SearchResult{
			ID:       r.Id,
			Score:    r.Score,
			Distance: r.Distance,
		}

		if r.Vector != nil {
			result.Vector = &Vector{
				ID:       r.Vector.Id,
				Values:   r.Vector.Values,
				Metadata: r.Vector.Metadata,
			}
		}

		results[i] = result
	}

	return results, nil
}

func (c *Client) Get(ctx context.Context, indexName string, vectorID string) (*Vector, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.GetRequest{
		IndexName: indexName,
		VectorId:  vectorID,
	}

	resp, err := c.client.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get failed: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get failed: %s", resp.Message)
	}

	if resp.Vector == nil {
		return nil, fmt.Errorf("vector not found")
	}

	return &Vector{
		ID:       resp.Vector.Id,
		Values:   resp.Vector.Values,
		Metadata: resp.Vector.Metadata,
	}, nil
}

func (c *Client) Update(ctx context.Context, indexName string, vectorID string, values []float32, metadata map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.UpdateRequest{
		IndexName: indexName,
		VectorId:  vectorID,
		Vector: &pb.Vector{
			Id:       vectorID,
			Values:   values,
			Metadata: metadata,
		},
	}

	resp, err := c.client.Update(ctx, req)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("update failed: %s", resp.Message)
	}

	return nil
}

func (c *Client) Delete(ctx context.Context, indexName string, vectorID string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.DeleteRequest{
		IndexName: indexName,
		VectorId:  vectorID,
	}

	resp, err := c.client.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("delete failed: %s", resp.Message)
	}

	return nil
}

func (c *Client) BatchDelete(ctx context.Context, indexName string, vectorIDs []string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.BatchDeleteRequest{
		IndexName: indexName,
		VectorIds: vectorIDs,
	}

	resp, err := c.client.BatchDelete(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("batch delete failed: %w", err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("batch delete failed: %s", resp.Message)
	}

	return int(resp.DeletedCount), nil
}

func (c *Client) CreateIndex(ctx context.Context, options CreateIndexOptions) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.CreateIndexRequest{
		IndexName:    options.Name,
		Dimension:    int32(options.Dimension),
		Metric:       options.Metric,
		ShardCount:   int32(options.ShardCount),
		ReplicaCount: int32(options.ReplicaCount),
		Options:      options.Options,
	}

	resp, err := c.client.CreateIndex(ctx, req)
	if err != nil {
		return fmt.Errorf("create index failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("create index failed: %s", resp.Message)
	}

	return nil
}

func (c *Client) DropIndex(ctx context.Context, indexName string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.DropIndexRequest{
		IndexName: indexName,
	}

	resp, err := c.client.DropIndex(ctx, req)
	if err != nil {
		return fmt.Errorf("drop index failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("drop index failed: %s", resp.Message)
	}

	return nil
}

func (c *Client) GetStats(ctx context.Context, indexName string) (*IndexStats, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req := &pb.GetStatsRequest{
		IndexName: indexName,
	}

	resp, err := c.client.GetStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get stats failed: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get stats failed: %s", resp.Message)
	}

	if resp.Stats == nil {
		return nil, fmt.Errorf("no stats available")
	}

	shardStats := make(map[int]*ShardStats)
	for id, s := range resp.Stats.ShardStats {
		shardStats[int(id)] = &ShardStats{
			ShardID:          int(s.ShardId),
			VectorCount:      s.VectorCount,
			Status:           s.Status,
			ReplicaNodes:     s.ReplicaNodes,
			MemoryUsageBytes: s.MemoryUsageBytes,
		}
	}

	return &IndexStats{
		VectorCount:      resp.Stats.VectorCount,
		Dimension:        int(resp.Stats.Dimension),
		Metric:           resp.Stats.Metric,
		ShardCount:       int(resp.Stats.ShardCount),
		ReplicaCount:     int(resp.Stats.ReplicaCount),
		MemoryUsageBytes: resp.Stats.MemoryUsageBytes,
		ShardStats:       shardStats,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

type Vector struct {
	ID       string
	Values   []float32
	Metadata map[string]string
}

type SearchOptions struct {
	TopK            int
	Filter          map[string]string
	MinScore        float32
	IncludeMetadata bool
}

type SearchResult struct {
	ID       string
	Score    float32
	Distance float32
	Vector   *Vector
}

type BatchInsertResult struct {
	Success       bool
	InsertedCount int
	FailedIDs     []string
}

type CreateIndexOptions struct {
	Name         string
	Dimension    int
	Metric       string
	ShardCount   int
	ReplicaCount int
	Options      map[string]string
}

type IndexStats struct {
	VectorCount      int64
	Dimension        int
	Metric           string
	ShardCount       int
	ReplicaCount     int
	MemoryUsageBytes int64
	ShardStats       map[int]*ShardStats
}

type ShardStats struct {
	ShardID          int
	VectorCount      int64
	Status           string
	ReplicaNodes     []string
	MemoryUsageBytes int64
}
