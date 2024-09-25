package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/elcruzo/vectorhub/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testServerAddr = "localhost:50051"
	testIndexName  = "test_index"
	testDimension  = 128
)

func TestVectorOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create client
	cfg := &client.ClientConfig{
		Address: testServerAddr,
		Timeout: 30 * time.Second,
	}

	c, err := client.NewClient(cfg)
	require.NoError(t, err, "Failed to create client")
	defer func() { _ = c.Close() }()

	ctx := context.Background()

	// Create index
	t.Run("CreateIndex", func(t *testing.T) {
		err := c.CreateIndex(ctx, client.CreateIndexOptions{
			Name:         testIndexName,
			Dimension:    testDimension,
			Metric:       "cosine",
			ShardCount:   4,
			ReplicaCount: 1,
		})
		assert.NoError(t, err, "Failed to create index")
	})

	// Insert vectors
	t.Run("InsertVectors", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			vector := make([]float32, testDimension)
			for j := range vector {
				vector[j] = float32(i+j) / 100.0
			}

			err := c.Insert(ctx, testIndexName, fmt.Sprintf("vec-%d", i), vector, map[string]string{
				"id":       fmt.Sprintf("%d", i),
				"category": "test",
			})
			assert.NoError(t, err, "Failed to insert vector %d", i)
		}
	})

	// Batch insert
	t.Run("BatchInsert", func(t *testing.T) {
		vectors := make([]client.Vector, 20)
		for i := 0; i < 20; i++ {
			values := make([]float32, testDimension)
			for j := range values {
				values[j] = float32(i*2+j) / 100.0
			}

			vectors[i] = client.Vector{
				ID:     fmt.Sprintf("batch-vec-%d", i),
				Values: values,
				Metadata: map[string]string{
					"batch": "true",
					"id":    fmt.Sprintf("%d", i),
				},
			}
		}

		result, err := c.BatchInsert(ctx, testIndexName, vectors, true)
		assert.NoError(t, err, "Failed to batch insert")
		assert.True(t, result.Success, "Batch insert failed")
		assert.Equal(t, 20, result.InsertedCount, "Expected 20 vectors inserted")
		assert.Empty(t, result.FailedIDs, "Expected no failed inserts")
	})

	// Get vector
	t.Run("GetVector", func(t *testing.T) {
		vector, err := c.Get(ctx, testIndexName, "vec-0")
		assert.NoError(t, err, "Failed to get vector")
		assert.NotNil(t, vector, "Vector should not be nil")
		assert.Equal(t, "vec-0", vector.ID, "Vector ID mismatch")
		assert.Equal(t, testDimension, len(vector.Values), "Vector dimension mismatch")
	})

	// Search vectors
	t.Run("SearchVectors", func(t *testing.T) {
		queryVector := make([]float32, testDimension)
		for i := range queryVector {
			queryVector[i] = float32(i) / 100.0
		}

		results, err := c.Search(ctx, testIndexName, queryVector, client.SearchOptions{
			TopK:            5,
			IncludeMetadata: true,
		})
		assert.NoError(t, err, "Failed to search")
		assert.NotEmpty(t, results, "Expected search results")
		assert.LessOrEqual(t, len(results), 5, "Expected at most 5 results")

		// Verify results are sorted by score
		for i := 1; i < len(results); i++ {
			assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score, "Results should be sorted by score")
		}
	})

	// Update vector
	t.Run("UpdateVector", func(t *testing.T) {
		newVector := make([]float32, testDimension)
		for i := range newVector {
			newVector[i] = float32(i) / 50.0
		}

		err := c.Update(ctx, testIndexName, "vec-0", newVector, map[string]string{
			"updated": "true",
		})
		assert.NoError(t, err, "Failed to update vector")

		// Verify update
		updated, err := c.Get(ctx, testIndexName, "vec-0")
		assert.NoError(t, err, "Failed to get updated vector")
		assert.Equal(t, "true", updated.Metadata["updated"], "Metadata not updated")
	})

	// Delete vector
	t.Run("DeleteVector", func(t *testing.T) {
		err := c.Delete(ctx, testIndexName, "vec-9")
		assert.NoError(t, err, "Failed to delete vector")

		// Verify deletion
		_, err = c.Get(ctx, testIndexName, "vec-9")
		assert.Error(t, err, "Expected error when getting deleted vector")
	})

	// Batch delete
	t.Run("BatchDelete", func(t *testing.T) {
		idsToDelete := []string{"batch-vec-0", "batch-vec-1", "batch-vec-2"}
		count, err := c.BatchDelete(ctx, testIndexName, idsToDelete)
		assert.NoError(t, err, "Failed to batch delete")
		assert.Equal(t, 3, count, "Expected 3 vectors deleted")
	})

	// Get stats
	t.Run("GetStats", func(t *testing.T) {
		stats, err := c.GetStats(ctx, testIndexName)
		assert.NoError(t, err, "Failed to get stats")
		assert.NotNil(t, stats, "Stats should not be nil")
		assert.Greater(t, stats.VectorCount, int64(0), "Expected some vectors")
		assert.Equal(t, testDimension, stats.Dimension, "Dimension mismatch")
		assert.Equal(t, "cosine", stats.Metric, "Metric mismatch")
	})

	// Drop index
	t.Run("DropIndex", func(t *testing.T) {
		err := c.DropIndex(ctx, testIndexName)
		assert.NoError(t, err, "Failed to drop index")
	})
}

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &client.ClientConfig{
		Address: testServerAddr,
		Timeout: 30 * time.Second,
	}

	c, err := client.NewClient(cfg)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	indexName := "concurrent_test_index"

	// Create index
	err = c.CreateIndex(ctx, client.CreateIndexOptions{
		Name:         indexName,
		Dimension:    64,
		Metric:       "euclidean",
		ShardCount:   4,
		ReplicaCount: 1,
	})
	require.NoError(t, err)
	defer func() { _ = c.DropIndex(ctx, indexName) }()

	// Concurrent inserts
	t.Run("ConcurrentInserts", func(t *testing.T) {
		numGoroutines := 10
		vectorsPerGoroutine := 10

		errChan := make(chan error, numGoroutines)

		for g := 0; g < numGoroutines; g++ {
			go func(goroutineID int) {
				for i := 0; i < vectorsPerGoroutine; i++ {
					vector := make([]float32, 64)
					for j := range vector {
						vector[j] = float32(goroutineID*100 + i + j)
					}

					err := c.Insert(ctx, indexName, fmt.Sprintf("concurrent-%d-%d", goroutineID, i), vector, nil)
					if err != nil {
						errChan <- err
						return
					}
				}
				errChan <- nil
			}(g)
		}

		// Wait for all goroutines
		for i := 0; i < numGoroutines; i++ {
			err := <-errChan
			assert.NoError(t, err, "Concurrent insert failed")
		}

		// Verify count
		stats, err := c.GetStats(ctx, indexName)
		assert.NoError(t, err)
		expectedCount := int64(numGoroutines * vectorsPerGoroutine)
		assert.Equal(t, expectedCount, stats.VectorCount, "Vector count mismatch after concurrent inserts")
	})
}
