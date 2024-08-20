package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/elcruzo/vectorhub/api/proto"
	"github.com/elcruzo/vectorhub/internal/metrics"
	"github.com/elcruzo/vectorhub/internal/replication"
	"github.com/elcruzo/vectorhub/internal/shard"
	"github.com/elcruzo/vectorhub/internal/storage"
	"go.uber.org/zap"
)

type VectorService struct {
	pb.UnimplementedVectorServiceServer
	shardManager       *shard.ShardManager
	replicationManager *replication.Manager
	metricsCollector   *metrics.Collector
	logger             *zap.Logger
	indexes            map[string]*storage.VectorIndex
	indexMu            sync.RWMutex
}

func NewVectorService(
	shardManager *shard.ShardManager,
	replicationManager *replication.Manager,
	metricsCollector *metrics.Collector,
	logger *zap.Logger,
) *VectorService {
	return &VectorService{
		shardManager:       shardManager,
		replicationManager: replicationManager,
		metricsCollector:   metricsCollector,
		logger:             logger,
		indexes:            make(map[string]*storage.VectorIndex),
	}
}

func (s *VectorService) Insert(ctx context.Context, req *pb.InsertRequest) (*pb.InsertResponse, error) {
	start := time.Now()
	defer func() {
		s.metricsCollector.RecordLatency("insert", time.Since(start))
	}()

	if req.Vector == nil || len(req.Vector.Values) == 0 {
		return &pb.InsertResponse{
			Success: false,
			Message: "vector is required",
		}, nil
	}

	s.indexMu.RLock()
	index, exists := s.indexes[req.IndexName]
	s.indexMu.RUnlock()

	if !exists {
		return &pb.InsertResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	if len(req.Vector.Values) != index.Dimension {
		return &pb.InsertResponse{
			Success: false,
			Message: fmt.Sprintf("vector dimension mismatch: expected %d, got %d", index.Dimension, len(req.Vector.Values)),
		}, nil
	}

	storedVector := &storage.StoredVector{
		VectorMetadata: storage.VectorMetadata{
			ID:        req.Vector.Id,
			Metadata:  req.Vector.Metadata,
			Timestamp: time.Now().UnixNano(),
		},
		Values: req.Vector.Values,
	}

	shardID, err := s.shardManager.GetShardForKey(req.Vector.Id)
	if err != nil {
		s.logger.Error("Failed to get shard for key", zap.Error(err))
		return &pb.InsertResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	adapter, err := s.shardManager.GetShardAdapter(shardID)
	if err != nil {
		s.logger.Error("Failed to get shard adapter", zap.Error(err))
		return &pb.InsertResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	if err := adapter.StoreVector(ctx, req.IndexName, storedVector); err != nil {
		s.logger.Error("Failed to store vector", zap.Error(err))
		return &pb.InsertResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	if err := s.replicationManager.ReplicateInsert(ctx, req.IndexName, storedVector, shardID); err != nil {
		s.logger.Warn("Failed to replicate insert", zap.Error(err))
	}

	s.metricsCollector.IncrementCounter("vectors_inserted", 1)

	return &pb.InsertResponse{
		Success:  true,
		Message:  "vector inserted successfully",
		VectorId: req.Vector.Id,
	}, nil
}

func (s *VectorService) BatchInsert(ctx context.Context, req *pb.BatchInsertRequest) (*pb.BatchInsertResponse, error) {
	start := time.Now()
	defer func() {
		s.metricsCollector.RecordLatency("batch_insert", time.Since(start))
	}()

	s.indexMu.RLock()
	index, exists := s.indexes[req.IndexName]
	s.indexMu.RUnlock()

	if !exists {
		return &pb.BatchInsertResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	shardGroups := make(map[int][]*storage.StoredVector)
	failedIDs := make([]string, 0)

	for _, vector := range req.Vectors {
		if len(vector.Values) != index.Dimension {
			failedIDs = append(failedIDs, vector.Id)
			continue
		}

		storedVector := &storage.StoredVector{
			VectorMetadata: storage.VectorMetadata{
				ID:        vector.Id,
				Metadata:  vector.Metadata,
				Timestamp: time.Now().UnixNano(),
			},
			Values: vector.Values,
		}

		shardID, err := s.shardManager.GetShardForKey(vector.Id)
		if err != nil {
			failedIDs = append(failedIDs, vector.Id)
			continue
		}

		if _, exists := shardGroups[shardID]; !exists {
			shardGroups[shardID] = make([]*storage.StoredVector, 0)
		}
		shardGroups[shardID] = append(shardGroups[shardID], storedVector)
	}

	inserted := 0

	if req.Parallel {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for shardID, vectors := range shardGroups {
			wg.Add(1)
			go func(sID int, vecs []*storage.StoredVector) {
				defer wg.Done()

				adapter, err := s.shardManager.GetShardAdapter(sID)
				if err != nil {
					mu.Lock()
					for _, v := range vecs {
						failedIDs = append(failedIDs, v.ID)
					}
					mu.Unlock()
					return
				}

				if err := adapter.BatchStoreVectors(ctx, req.IndexName, vecs); err != nil {
					mu.Lock()
					for _, v := range vecs {
						failedIDs = append(failedIDs, v.ID)
					}
					mu.Unlock()
				} else {
					mu.Lock()
					inserted += len(vecs)
					mu.Unlock()

					_ = s.replicationManager.ReplicateBatch(ctx, req.IndexName, vecs, sID)
				}
			}(shardID, vectors)
		}

		wg.Wait()
	} else {
		for shardID, vectors := range shardGroups {
			adapter, err := s.shardManager.GetShardAdapter(shardID)
			if err != nil {
				for _, v := range vectors {
					failedIDs = append(failedIDs, v.ID)
				}
				continue
			}

			if err := adapter.BatchStoreVectors(ctx, req.IndexName, vectors); err != nil {
				for _, v := range vectors {
					failedIDs = append(failedIDs, v.ID)
				}
			} else {
				inserted += len(vectors)
				s.replicationManager.ReplicateBatch(ctx, req.IndexName, vectors, shardID)
			}
		}
	}

	s.metricsCollector.IncrementCounter("vectors_inserted", float64(inserted))

	return &pb.BatchInsertResponse{
		Success:       inserted > 0,
		Message:       fmt.Sprintf("inserted %d vectors", inserted),
		InsertedCount: int32(inserted),
		FailedIds:     failedIDs,
	}, nil
}

func (s *VectorService) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	start := time.Now()
	defer func() {
		s.metricsCollector.RecordLatency("search", time.Since(start))
	}()

	s.indexMu.RLock()
	index, exists := s.indexes[req.IndexName]
	s.indexMu.RUnlock()

	if !exists {
		return &pb.SearchResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	if len(req.QueryVector) != index.Dimension {
		return &pb.SearchResponse{
			Success: false,
			Message: fmt.Sprintf("query vector dimension mismatch: expected %d, got %d", index.Dimension, len(req.QueryVector)),
		}, nil
	}

	shards := s.shardManager.GetHealthyShards()
	if len(shards) == 0 {
		return &pb.SearchResponse{
			Success: false,
			Message: "no healthy shards available",
		}, nil
	}

	topK := int(req.TopK)
	if topK <= 0 {
		topK = 10
	}

	allResults := make([]*storage.SearchResult, 0)
	resultsChan := make(chan []*storage.SearchResult, len(shards))
	errorsChan := make(chan error, len(shards))

	var wg sync.WaitGroup
	for _, shardInfo := range shards {
		wg.Add(1)
		go func(sID int) {
			defer wg.Done()

			adapter, err := s.shardManager.GetShardAdapter(sID)
			if err != nil {
				errorsChan <- err
				return
			}

			results, err := adapter.SearchVectors(ctx, req.IndexName, req.QueryVector, topK, req.Filter)
			if err != nil {
				errorsChan <- err
				return
			}

			resultsChan <- results
		}(shardInfo.ID)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
		close(errorsChan)
	}()

	for results := range resultsChan {
		allResults = append(allResults, results...)
	}

	if len(allResults) > topK {
		storage.QuickSelectResults(allResults, topK)
		allResults = allResults[:topK]
	}

	storage.SortResultsByScore(allResults)

	pbResults := make([]*pb.SearchResult, 0, len(allResults))
	for _, r := range allResults {
		result := &pb.SearchResult{
			Id:       r.ID,
			Score:    r.Score,
			Distance: r.Distance,
		}

		if req.IncludeMetadata && r.Vector != nil {
			result.Vector = &pb.Vector{
				Id:        r.Vector.ID,
				Values:    r.Vector.Values,
				Metadata:  r.Vector.Metadata,
				Timestamp: r.Vector.Timestamp,
			}
		}

		if req.MinScore > 0 && r.Score < req.MinScore {
			continue
		}

		pbResults = append(pbResults, result)
	}

	s.metricsCollector.IncrementCounter("searches", 1)

	return &pb.SearchResponse{
		Success:      true,
		Message:      "search completed",
		Results:      pbResults,
		SearchTimeMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *VectorService) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	shardID, err := s.shardManager.GetShardForKey(req.VectorId)
	if err != nil {
		return &pb.GetResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	adapter, err := s.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return &pb.GetResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	vector, err := adapter.GetVector(ctx, req.IndexName, req.VectorId)
	if err != nil {
		return &pb.GetResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.GetResponse{
		Success: true,
		Message: "vector retrieved",
		Vector: &pb.Vector{
			Id:        vector.ID,
			Values:    vector.Values,
			Metadata:  vector.Metadata,
			Timestamp: vector.Timestamp,
		},
	}, nil
}

func (s *VectorService) Update(ctx context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	s.indexMu.RLock()
	index, exists := s.indexes[req.IndexName]
	s.indexMu.RUnlock()

	if !exists {
		return &pb.UpdateResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	if len(req.Vector.Values) != index.Dimension {
		return &pb.UpdateResponse{
			Success: false,
			Message: fmt.Sprintf("vector dimension mismatch: expected %d, got %d", index.Dimension, len(req.Vector.Values)),
		}, nil
	}

	storedVector := &storage.StoredVector{
		VectorMetadata: storage.VectorMetadata{
			ID:        req.VectorId,
			Metadata:  req.Vector.Metadata,
			Timestamp: time.Now().UnixNano(),
		},
		Values: req.Vector.Values,
	}

	shardID, err := s.shardManager.GetShardForKey(req.VectorId)
	if err != nil {
		return &pb.UpdateResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	adapter, err := s.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return &pb.UpdateResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	if err := adapter.DeleteVector(ctx, req.IndexName, req.VectorId); err != nil {
		s.logger.Warn("Failed to delete old vector", zap.Error(err))
	}

	if err := adapter.StoreVector(ctx, req.IndexName, storedVector); err != nil {
		return &pb.UpdateResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	_ = s.replicationManager.ReplicateUpdate(ctx, req.IndexName, storedVector, shardID)

	return &pb.UpdateResponse{
		Success: true,
		Message: "vector updated successfully",
	}, nil
}

func (s *VectorService) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	shardID, err := s.shardManager.GetShardForKey(req.VectorId)
	if err != nil {
		return &pb.DeleteResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	adapter, err := s.shardManager.GetShardAdapter(shardID)
	if err != nil {
		return &pb.DeleteResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	if err := adapter.DeleteVector(ctx, req.IndexName, req.VectorId); err != nil {
		return &pb.DeleteResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	_ = s.replicationManager.ReplicateDelete(ctx, req.IndexName, req.VectorId, shardID)
	s.metricsCollector.IncrementCounter("vectors_deleted", 1)

	return &pb.DeleteResponse{
		Success: true,
		Message: "vector deleted successfully",
	}, nil
}

func (s *VectorService) BatchDelete(ctx context.Context, req *pb.BatchDeleteRequest) (*pb.BatchDeleteResponse, error) {
	shardGroups := make(map[int][]string)

	for _, vectorID := range req.VectorIds {
		shardID, err := s.shardManager.GetShardForKey(vectorID)
		if err != nil {
			continue
		}

		if _, exists := shardGroups[shardID]; !exists {
			shardGroups[shardID] = make([]string, 0)
		}
		shardGroups[shardID] = append(shardGroups[shardID], vectorID)
	}

	totalDeleted := 0
	for shardID, ids := range shardGroups {
		adapter, err := s.shardManager.GetShardAdapter(shardID)
		if err != nil {
			continue
		}

		deleted, err := adapter.BatchDeleteVectors(ctx, req.IndexName, ids)
		if err != nil {
			s.logger.Warn("Failed to batch delete vectors", zap.Error(err))
		} else {
			totalDeleted += deleted
			_ = s.replicationManager.ReplicateBatchDelete(ctx, req.IndexName, ids, shardID)
		}
	}

	s.metricsCollector.IncrementCounter("vectors_deleted", float64(totalDeleted))

	return &pb.BatchDeleteResponse{
		Success:      totalDeleted > 0,
		Message:      fmt.Sprintf("deleted %d vectors", totalDeleted),
		DeletedCount: int32(totalDeleted),
	}, nil
}

func (s *VectorService) CreateIndex(ctx context.Context, req *pb.CreateIndexRequest) (*pb.CreateIndexResponse, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if _, exists := s.indexes[req.IndexName]; exists {
		return &pb.CreateIndexResponse{
			Success: false,
			Message: fmt.Sprintf("index %s already exists", req.IndexName),
		}, nil
	}

	index := storage.NewVectorIndex(
		req.IndexName,
		int(req.Dimension),
		req.Metric,
		int(req.ShardCount),
		int(req.ReplicaCount),
	)

	if err := index.Validate(); err != nil {
		return &pb.CreateIndexResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	for k, v := range req.Options {
		index.Options[k] = v
	}

	for _, shardInfo := range s.shardManager.GetAllShards() {
		adapter, err := s.shardManager.GetShardAdapter(shardInfo.ID)
		if err != nil {
			continue
		}

		if err := adapter.CreateIndex(ctx, index); err != nil {
			s.logger.Error("Failed to create index on shard",
				zap.Int("shard", shardInfo.ID),
				zap.Error(err))
		}
	}

	s.indexes[req.IndexName] = index
	s.metricsCollector.IncrementCounter("indexes_created", 1)

	return &pb.CreateIndexResponse{
		Success: true,
		Message: fmt.Sprintf("index %s created successfully", req.IndexName),
	}, nil
}

func (s *VectorService) DropIndex(ctx context.Context, req *pb.DropIndexRequest) (*pb.DropIndexResponse, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if _, exists := s.indexes[req.IndexName]; !exists {
		return &pb.DropIndexResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	for _, shardInfo := range s.shardManager.GetAllShards() {
		adapter, err := s.shardManager.GetShardAdapter(shardInfo.ID)
		if err != nil {
			continue
		}

		if err := adapter.DropIndex(ctx, req.IndexName); err != nil {
			s.logger.Warn("Failed to drop index on shard",
				zap.Int("shard", shardInfo.ID),
				zap.Error(err))
		}
	}

	delete(s.indexes, req.IndexName)
	s.metricsCollector.IncrementCounter("indexes_dropped", 1)

	return &pb.DropIndexResponse{
		Success: true,
		Message: fmt.Sprintf("index %s dropped successfully", req.IndexName),
	}, nil
}

func (s *VectorService) GetStats(ctx context.Context, req *pb.GetStatsRequest) (*pb.GetStatsResponse, error) {
	s.indexMu.RLock()
	index, exists := s.indexes[req.IndexName]
	s.indexMu.RUnlock()

	if !exists {
		return &pb.GetStatsResponse{
			Success: false,
			Message: fmt.Sprintf("index %s not found", req.IndexName),
		}, nil
	}

	shardStats := make(map[int32]*pb.ShardStats)
	totalVectors := int64(0)
	totalMemory := int64(0)

	for _, shardInfo := range s.shardManager.GetAllShards() {
		adapter, err := s.shardManager.GetShardAdapter(shardInfo.ID)
		if err != nil {
			continue
		}

		count, err := adapter.GetVectorCount(ctx, req.IndexName)
		if err != nil {
			continue
		}

		shardStat := &pb.ShardStats{
			ShardId:          int32(shardInfo.ID),
			VectorCount:      count,
			Status:           shardInfo.Status,
			ReplicaNodes:     shardInfo.Replicas,
			MemoryUsageBytes: shardInfo.MemoryUsage,
		}

		shardStats[int32(shardInfo.ID)] = shardStat
		totalVectors += count
		totalMemory += shardInfo.MemoryUsage
	}

	return &pb.GetStatsResponse{
		Success: true,
		Message: "stats retrieved",
		Stats: &pb.IndexStats{
			VectorCount:      totalVectors,
			Dimension:        int32(index.Dimension),
			Metric:           index.Metric,
			ShardCount:       int32(index.ShardCount),
			ReplicaCount:     int32(index.ReplicaCount),
			MemoryUsageBytes: totalMemory,
			ShardStats:       shardStats,
		},
	}, nil
}

func (s *VectorService) StreamInsert(stream pb.VectorService_StreamInsertServer) error {
	s.logger.Info("StreamInsert: Starting streaming insert session")

	count := 0
	successCount := 0

	for {
		req, err := stream.Recv()
		if err == nil {
			break
		}
		if err != nil {
			s.logger.Error("StreamInsert: Failed to receive", zap.Error(err))
			return err
		}

		count++

		if req.Vector == nil || len(req.Vector.Values) == 0 {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: "vector is required",
			}); err != nil {
				return err
			}
			continue
		}

		s.indexMu.RLock()
		index, exists := s.indexes[req.IndexName]
		s.indexMu.RUnlock()

		if !exists {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: fmt.Sprintf("index %s not found", req.IndexName),
			}); err != nil {
				return err
			}
			continue
		}

		if len(req.Vector.Values) != index.Dimension {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: fmt.Sprintf("vector dimension mismatch: expected %d, got %d", index.Dimension, len(req.Vector.Values)),
			}); err != nil {
				return err
			}
			continue
		}

		storedVector := &storage.StoredVector{
			VectorMetadata: storage.VectorMetadata{
				ID:        req.Vector.Id,
				Metadata:  req.Vector.Metadata,
				Timestamp: time.Now().UnixNano(),
			},
			Values: req.Vector.Values,
		}

		shardID, err := s.shardManager.GetShardForKey(req.Vector.Id)
		if err != nil {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: err.Error(),
			}); err != nil {
				return err
			}
			continue
		}

		adapter, err := s.shardManager.GetShardAdapter(shardID)
		if err != nil {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: err.Error(),
			}); err != nil {
				return err
			}
			continue
		}

		ctx := stream.Context()
		if err := adapter.StoreVector(ctx, req.IndexName, storedVector); err != nil {
			if err := stream.Send(&pb.InsertResponse{
				Success: false,
				Message: err.Error(),
			}); err != nil {
				return err
			}
			continue
		}

		_ = s.replicationManager.ReplicateInsert(ctx, req.IndexName, storedVector, shardID)

		successCount++
		if err := stream.Send(&pb.InsertResponse{
			Success:  true,
			Message:  "vector inserted successfully",
			VectorId: req.Vector.Id,
		}); err != nil {
			return err
		}
	}

	s.metricsCollector.IncrementCounter("vectors_inserted", float64(successCount))
	s.logger.Info("StreamInsert: Completed",
		zap.Int("total", count),
		zap.Int("success", successCount))

	return nil
}

func (s *VectorService) StreamSearch(stream pb.VectorService_StreamSearchServer) error {
	s.logger.Info("StreamSearch: Starting streaming search session")

	count := 0

	for {
		req, err := stream.Recv()
		if err == nil {
			break
		}
		if err != nil {
			s.logger.Error("StreamSearch: Failed to receive", zap.Error(err))
			return err
		}

		count++

		s.indexMu.RLock()
		index, exists := s.indexes[req.IndexName]
		s.indexMu.RUnlock()

		if !exists {
			if err := stream.Send(&pb.SearchResponse{
				Success: false,
				Message: fmt.Sprintf("index %s not found", req.IndexName),
			}); err != nil {
				return err
			}
			continue
		}

		if len(req.QueryVector) != index.Dimension {
			if err := stream.Send(&pb.SearchResponse{
				Success: false,
				Message: fmt.Sprintf("query vector dimension mismatch: expected %d, got %d", index.Dimension, len(req.QueryVector)),
			}); err != nil {
				return err
			}
			continue
		}

		shards := s.shardManager.GetHealthyShards()
		if len(shards) == 0 {
			if err := stream.Send(&pb.SearchResponse{
				Success: false,
				Message: "no healthy shards available",
			}); err != nil {
				return err
			}
			continue
		}

		topK := int(req.TopK)
		if topK <= 0 {
			topK = 10
		}

		allResults := make([]*storage.SearchResult, 0)
		resultsChan := make(chan []*storage.SearchResult, len(shards))
		errorsChan := make(chan error, len(shards))

		var wg sync.WaitGroup
		ctx := stream.Context()

		for _, shardInfo := range shards {
			wg.Add(1)
			go func(sID int) {
				defer wg.Done()

				adapter, err := s.shardManager.GetShardAdapter(sID)
				if err != nil {
					errorsChan <- err
					return
				}

				results, err := adapter.SearchVectors(ctx, req.IndexName, req.QueryVector, topK, req.Filter)
				if err != nil {
					errorsChan <- err
					return
				}

				resultsChan <- results
			}(shardInfo.ID)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
			close(errorsChan)
		}()

		for results := range resultsChan {
			allResults = append(allResults, results...)
		}

		if len(allResults) > topK {
			storage.QuickSelectResults(allResults, topK)
			allResults = allResults[:topK]
		}

		storage.SortResultsByScore(allResults)

		pbResults := make([]*pb.SearchResult, 0, len(allResults))
		for _, r := range allResults {
			if req.MinScore > 0 && r.Score < req.MinScore {
				continue
			}

			result := &pb.SearchResult{
				Id:       r.ID,
				Score:    r.Score,
				Distance: r.Distance,
			}

			if req.IncludeMetadata && r.Vector != nil {
				result.Vector = &pb.Vector{
					Id:        r.Vector.ID,
					Values:    r.Vector.Values,
					Metadata:  r.Vector.Metadata,
					Timestamp: r.Vector.Timestamp,
				}
			}

			pbResults = append(pbResults, result)
		}

		if err := stream.Send(&pb.SearchResponse{
			Success: true,
			Message: "search completed",
			Results: pbResults,
		}); err != nil {
			return err
		}

		s.metricsCollector.IncrementCounter("searches", 1)
	}

	s.logger.Info("StreamSearch: Completed", zap.Int("searches", count))
	return nil
}

func (s *VectorService) StreamBatchInsert(stream pb.VectorService_StreamBatchInsertServer) error {
	s.logger.Info("StreamBatchInsert: Starting streaming batch insert session")

	totalCount := 0
	totalSuccess := 0

	for {
		req, err := stream.Recv()
		if err == nil {
			break
		}
		if err != nil {
			s.logger.Error("StreamBatchInsert: Failed to receive", zap.Error(err))
			return err
		}

		totalCount += len(req.Vectors)

		s.indexMu.RLock()
		index, exists := s.indexes[req.IndexName]
		s.indexMu.RUnlock()

		if !exists {
			if err := stream.Send(&pb.BatchInsertResponse{
				Success: false,
				Message: fmt.Sprintf("index %s not found", req.IndexName),
			}); err != nil {
				return err
			}
			continue
		}

		shardGroups := make(map[int][]*storage.StoredVector)
		failedIDs := make([]string, 0)

		for _, vector := range req.Vectors {
			if len(vector.Values) != index.Dimension {
				failedIDs = append(failedIDs, vector.Id)
				continue
			}

			storedVector := &storage.StoredVector{
				VectorMetadata: storage.VectorMetadata{
					ID:        vector.Id,
					Metadata:  vector.Metadata,
					Timestamp: time.Now().UnixNano(),
				},
				Values: vector.Values,
			}

			shardID, err := s.shardManager.GetShardForKey(vector.Id)
			if err != nil {
				failedIDs = append(failedIDs, vector.Id)
				continue
			}

			if _, exists := shardGroups[shardID]; !exists {
				shardGroups[shardID] = make([]*storage.StoredVector, 0)
			}
			shardGroups[shardID] = append(shardGroups[shardID], storedVector)
		}

		inserted := 0
		ctx := stream.Context()

		if req.Parallel {
			var wg sync.WaitGroup
			var mu sync.Mutex

			for shardID, vectors := range shardGroups {
				wg.Add(1)
				go func(sID int, vecs []*storage.StoredVector) {
					defer wg.Done()

					adapter, err := s.shardManager.GetShardAdapter(sID)
					if err != nil {
						mu.Lock()
						for _, v := range vecs {
							failedIDs = append(failedIDs, v.ID)
						}
						mu.Unlock()
						return
					}

					if err := adapter.BatchStoreVectors(ctx, req.IndexName, vecs); err != nil {
						mu.Lock()
						for _, v := range vecs {
							failedIDs = append(failedIDs, v.ID)
						}
						mu.Unlock()
					} else {
						mu.Lock()
						inserted += len(vecs)
						mu.Unlock()

						_ = s.replicationManager.ReplicateBatch(ctx, req.IndexName, vecs, sID)
					}
				}(shardID, vectors)
			}

			wg.Wait()
		} else {
			for shardID, vectors := range shardGroups {
				adapter, err := s.shardManager.GetShardAdapter(shardID)
				if err != nil {
					for _, v := range vectors {
						failedIDs = append(failedIDs, v.ID)
					}
					continue
				}

				if err := adapter.BatchStoreVectors(ctx, req.IndexName, vectors); err != nil {
					for _, v := range vectors {
						failedIDs = append(failedIDs, v.ID)
					}
				} else {
					inserted += len(vectors)
					s.replicationManager.ReplicateBatch(ctx, req.IndexName, vectors, shardID)
				}
			}
		}

		totalSuccess += inserted

		if err := stream.Send(&pb.BatchInsertResponse{
			Success:       inserted > 0,
			Message:       fmt.Sprintf("inserted %d vectors", inserted),
			InsertedCount: int32(inserted),
			FailedIds:     failedIDs,
		}); err != nil {
			return err
		}

		s.metricsCollector.IncrementCounter("vectors_inserted", float64(inserted))
	}

	s.logger.Info("StreamBatchInsert: Completed",
		zap.Int("total", totalCount),
		zap.Int("success", totalSuccess))

	return nil
}
