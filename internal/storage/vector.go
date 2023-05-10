package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type VectorMetadata struct {
	ID        string            `json:"id"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

type StoredVector struct {
	VectorMetadata
	Values []float32 `json:"values"`
}

func (v *StoredVector) ToBytes() ([]byte, error) {
	return json.Marshal(v)
}

func VectorFromBytes(data []byte) (*StoredVector, error) {
	var v StoredVector
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func SerializeVector(values []float32) []byte {
	buf := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func DeserializeVector(data []byte) []float32 {
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return values
}

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return math.MaxFloat32
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

func DotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var product float32
	for i := range a {
		product += a[i] * b[i]
	}

	return product
}

type DistanceFunc func(a, b []float32) float32

func GetDistanceFunc(metric string) DistanceFunc {
	switch metric {
	case "cosine":
		return func(a, b []float32) float32 {
			return 1 - CosineSimilarity(a, b)
		}
	case "euclidean":
		return EuclideanDistance
	case "dot_product":
		return func(a, b []float32) float32 {
			return -DotProduct(a, b)
		}
	default:
		return EuclideanDistance
	}
}

type SearchResult struct {
	ID       string
	Score    float32
	Distance float32
	Vector   *StoredVector
}

type VectorIndex struct {
	Name         string
	Dimension    int
	Metric       string
	ShardCount   int
	ReplicaCount int
	CreatedAt    time.Time
	Options      map[string]string
}

func NewVectorIndex(name string, dimension int, metric string, shardCount, replicaCount int) *VectorIndex {
	return &VectorIndex{
		Name:         name,
		Dimension:    dimension,
		Metric:       metric,
		ShardCount:   shardCount,
		ReplicaCount: replicaCount,
		CreatedAt:    time.Now(),
		Options:      make(map[string]string),
	}
}

func (idx *VectorIndex) Validate() error {
	if idx.Dimension <= 0 {
		return fmt.Errorf("dimension must be positive")
	}
	if idx.ShardCount <= 0 {
		return fmt.Errorf("shard count must be positive")
	}
	if idx.ReplicaCount < 0 {
		return fmt.Errorf("replica count cannot be negative")
	}
	if idx.Metric != "cosine" && idx.Metric != "euclidean" && idx.Metric != "dot_product" {
		return fmt.Errorf("invalid metric: %s", idx.Metric)
	}
	return nil
}