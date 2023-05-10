package unit

import (
	"testing"

	"github.com/elcruzo/vectorhub/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorSerialization(t *testing.T) {
	vector := &storage.StoredVector{
		VectorMetadata: storage.VectorMetadata{
			ID: "test-vector-1",
			Metadata: map[string]string{
				"category": "test",
				"source":   "unit-test",
			},
			Timestamp: 1234567890,
		},
		Values: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
	}

	data, err := vector.ToBytes()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	recovered, err := storage.VectorFromBytes(data)
	require.NoError(t, err)
	assert.Equal(t, vector.ID, recovered.ID)
	assert.Equal(t, vector.Values, recovered.Values)
	assert.Equal(t, vector.Metadata, recovered.Metadata)
	assert.Equal(t, vector.Timestamp, recovered.Timestamp)
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "different lengths",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storage.CosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0.0,
		},
		{
			name:     "simple distance",
			a:        []float32{0, 0, 0},
			b:        []float32{3, 4, 0},
			expected: 5.0,
		},
		{
			name:     "3D distance",
			a:        []float32{1, 2, 3},
			b:        []float32{4, 6, 8},
			expected: 7.071068,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storage.EuclideanDistance(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "simple dot product",
			a:        []float32{1, 2, 3},
			b:        []float32{4, 5, 6},
			expected: 32.0,
		},
		{
			name:     "zero vector",
			a:        []float32{1, 2, 3},
			b:        []float32{0, 0, 0},
			expected: 0.0,
		},
		{
			name:     "negative values",
			a:        []float32{1, -2, 3},
			b:        []float32{-4, 5, -6},
			expected: -32.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storage.DotProduct(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestGetDistanceFunc(t *testing.T) {
	tests := []struct {
		metric string
	}{
		{"cosine"},
		{"euclidean"},
		{"dot_product"},
		{"unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			distFunc := storage.GetDistanceFunc(tt.metric)
			assert.NotNil(t, distFunc)
			
			a := []float32{1, 2, 3}
			b := []float32{4, 5, 6}
			result := distFunc(a, b)
			assert.NotZero(t, result)
		})
	}
}

func TestVectorIndex(t *testing.T) {
	index := storage.NewVectorIndex("test-index", 128, "cosine", 4, 2)
	
	assert.Equal(t, "test-index", index.Name)
	assert.Equal(t, 128, index.Dimension)
	assert.Equal(t, "cosine", index.Metric)
	assert.Equal(t, 4, index.ShardCount)
	assert.Equal(t, 2, index.ReplicaCount)
	assert.NotNil(t, index.Options)
	assert.NotZero(t, index.CreatedAt)
}

func TestVectorIndexValidation(t *testing.T) {
	tests := []struct {
		name      string
		index     *storage.VectorIndex
		wantError bool
	}{
		{
			name: "valid index",
			index: &storage.VectorIndex{
				Dimension:    128,
				ShardCount:   4,
				ReplicaCount: 2,
				Metric:       "cosine",
			},
			wantError: false,
		},
		{
			name: "invalid dimension",
			index: &storage.VectorIndex{
				Dimension:    0,
				ShardCount:   4,
				ReplicaCount: 2,
				Metric:       "cosine",
			},
			wantError: true,
		},
		{
			name: "invalid shard count",
			index: &storage.VectorIndex{
				Dimension:    128,
				ShardCount:   0,
				ReplicaCount: 2,
				Metric:       "cosine",
			},
			wantError: true,
		},
		{
			name: "invalid replica count",
			index: &storage.VectorIndex{
				Dimension:    128,
				ShardCount:   4,
				ReplicaCount: -1,
				Metric:       "cosine",
			},
			wantError: true,
		},
		{
			name: "invalid metric",
			index: &storage.VectorIndex{
				Dimension:    128,
				ShardCount:   4,
				ReplicaCount: 2,
				Metric:       "invalid",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.index.Validate()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkCosineSimilarity(b *testing.B) {
	a := make([]float32, 128)
	c := make([]float32, 128)
	for i := range a {
		a[i] = float32(i) / 128.0
		c[i] = float32(i+1) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.CosineSimilarity(a, c)
	}
}

func BenchmarkEuclideanDistance(b *testing.B) {
	a := make([]float32, 128)
	c := make([]float32, 128)
	for i := range a {
		a[i] = float32(i) / 128.0
		c[i] = float32(i+1) / 128.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		storage.EuclideanDistance(a, c)
	}
}

func BenchmarkVectorSerialization(b *testing.B) {
	vector := &storage.StoredVector{
		VectorMetadata: storage.VectorMetadata{
			ID:        "test-vector",
			Metadata:  map[string]string{"key": "value"},
			Timestamp: 1234567890,
		},
		Values: make([]float32, 128),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := vector.ToBytes()
		storage.VectorFromBytes(data)
	}
}