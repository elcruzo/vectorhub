package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/elcruzo/vectorhub/pkg/client"
)

const (
	benchServerAddr = "localhost:50051"
	benchIndexName  = "bench_index"
	benchDimension  = 128
)

var benchClient *client.Client

func setupBenchmark(b *testing.B) {
	if benchClient == nil {
		cfg := &client.Config{
			Address: benchServerAddr,
			Timeout: 60 * time.Second,
		}

		var err error
		benchClient, err = client.NewClient(cfg)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}

		ctx := context.Background()
		// Create index
		err = benchClient.CreateIndex(ctx, client.CreateIndexOptions{
			Name:         benchIndexName,
			Dimension:    benchDimension,
			Metric:       "cosine",
			ShardCount:   8,
			ReplicaCount: 2,
		})
		if err != nil {
			b.Logf("Index may already exist: %v", err)
		}
	}
}

func teardownBenchmark(b *testing.B) {
	// Keep the index for multiple benchmarks
	// Call benchClient.DropIndex(context.Background(), benchIndexName) manually if needed
}

func BenchmarkInsert(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vector := generateRandomVector(benchDimension)
		err := benchClient.Insert(ctx, benchIndexName, fmt.Sprintf("bench-insert-%d", i), vector, map[string]string{
			"benchmark": "insert",
		})
		if err != nil {
			b.Errorf("Insert failed: %v", err)
		}
	}
}

func BenchmarkBatchInsert(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize-%d", batchSize), func(b *testing.B) {
			ctx := context.Background()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				vectors := make([]client.Vector, batchSize)
				for j := 0; j < batchSize; j++ {
					vectors[j] = client.Vector{
						ID:     fmt.Sprintf("bench-batch-%d-%d", i, j),
						Values: generateRandomVector(benchDimension),
						Metadata: map[string]string{
							"benchmark": "batch",
							"batch":     fmt.Sprintf("%d", i),
						},
					}
				}

				_, err := benchClient.BatchInsert(ctx, benchIndexName, vectors, true)
				if err != nil {
					b.Errorf("Batch insert failed: %v", err)
				}
			}

			b.ReportMetric(float64(batchSize*b.N)/b.Elapsed().Seconds(), "vectors/sec")
		})
	}
}

func BenchmarkSearch(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	// Pre-populate some vectors
	ctx := context.Background()
	numVectors := 10000
	b.Logf("Pre-populating %d vectors...", numVectors)

	vectors := make([]client.Vector, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = client.Vector{
			ID:     fmt.Sprintf("bench-search-data-%d", i),
			Values: generateRandomVector(benchDimension),
		}
	}

	// Insert in batches
	batchSize := 1000
	for i := 0; i < numVectors; i += batchSize {
		end := i + batchSize
		if end > numVectors {
			end = numVectors
		}
		_, _ = benchClient.BatchInsert(ctx, benchIndexName, vectors[i:end], true)
	}

	topKValues := []int{1, 5, 10, 50, 100}

	for _, topK := range topKValues {
		b.Run(fmt.Sprintf("TopK-%d", topK), func(b *testing.B) {
			queryVector := generateRandomVector(benchDimension)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := benchClient.Search(ctx, benchIndexName, queryVector, client.SearchOptions{
					TopK:            topK,
					IncludeMetadata: false,
				})
				if err != nil {
					b.Errorf("Search failed: %v", err)
				}
			}

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "searches/sec")
		})
	}
}

func BenchmarkSearchWithMetadata(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()
	queryVector := generateRandomVector(benchDimension)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := benchClient.Search(ctx, benchIndexName, queryVector, client.SearchOptions{
			TopK:            10,
			IncludeMetadata: true,
		})
		if err != nil {
			b.Errorf("Search failed: %v", err)
		}
	}
}

func BenchmarkGet(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()

	// Insert a test vector
	testID := "bench-get-test"
	_, _ = benchClient.Insert(ctx, benchIndexName, testID, generateRandomVector(benchDimension), nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := benchClient.Get(ctx, benchIndexName, testID)
		if err != nil {
			b.Errorf("Get failed: %v", err)
		}
	}
}

func BenchmarkUpdate(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()
	testID := "bench-update-test"

	// Insert initial vector
	_, _ = benchClient.Insert(ctx, benchIndexName, testID, generateRandomVector(benchDimension), nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vector := generateRandomVector(benchDimension)
		err := benchClient.Update(ctx, benchIndexName, testID, vector, map[string]string{
			"update_count": fmt.Sprintf("%d", i),
		})
		if err != nil {
			b.Errorf("Update failed: %v", err)
		}
	}
}

func BenchmarkDelete(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()

	// Pre-insert vectors to delete
	for i := 0; i < b.N; i++ {
		_, _ = benchClient.Insert(ctx, benchIndexName, fmt.Sprintf("bench-delete-%d", i), generateRandomVector(benchDimension), nil)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := benchClient.Delete(ctx, benchIndexName, fmt.Sprintf("bench-delete-%d", i))
		if err != nil {
			b.Errorf("Delete failed: %v", err)
		}
	}
}

func BenchmarkConcurrentInserts(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			vector := generateRandomVector(benchDimension)
			err := benchClient.Insert(ctx, benchIndexName, fmt.Sprintf("bench-concurrent-%d", i), vector, nil)
			if err != nil {
				b.Errorf("Concurrent insert failed: %v", err)
			}
			i++
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "concurrent_inserts/sec")
}

func BenchmarkConcurrentSearches(b *testing.B) {
	setupBenchmark(b)
	defer teardownBenchmark(b)

	ctx := context.Background()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		queryVector := generateRandomVector(benchDimension)
		for pb.Next() {
			_, err := benchClient.Search(ctx, benchIndexName, queryVector, client.SearchOptions{
				TopK: 10,
			})
			if err != nil {
				b.Errorf("Concurrent search failed: %v", err)
			}
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "concurrent_searches/sec")
}

// Helper function to generate random vectors
func generateRandomVector(dimension int) []float32 {
	vector := make([]float32, dimension)
	for i := range vector {
		vector[i] = rand.Float32()
	}
	return vector
}

func init() {
	// Seed is deprecated in Go 1.20+, using global random generator
}
