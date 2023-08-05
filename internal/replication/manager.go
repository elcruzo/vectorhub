package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/elcruzo/vectorhub/internal/storage"
	"go.uber.org/zap"
)

type ReplicaNode struct {
	ID       string
	Address  string
	Role     string // primary, secondary
	Status   string // active, syncing, failed
	Lag      time.Duration
	LastSync time.Time
}

type Manager struct {
	replicas       map[int][]*ReplicaNode // shardID -> replicas
	primaryNodes   map[int]string         // shardID -> primary address
	adapters       map[string]*storage.RedisAdapter
	config         *Config
	logger         *zap.Logger
	mu             sync.RWMutex
	syncTicker     *time.Ticker
	ctx            context.Context
	cancel         context.CancelFunc
	replicationLog *ReplicationLog
}

type Config struct {
	ReplicationFactor int
	SyncInterval      time.Duration
	MaxLag            time.Duration
	FailoverTimeout   time.Duration
	RedisPassword     string
	RedisDB           int
}

type ReplicationLog struct {
	entries []ReplicationEntry
	mu      sync.RWMutex
}

type ReplicationEntry struct {
	Timestamp time.Time
	ShardID   int
	Operation string
	Key       string
	Success   bool
}

func NewManager(config *Config, logger *zap.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		replicas:       make(map[int][]*ReplicaNode),
		primaryNodes:   make(map[int]string),
		adapters:       make(map[string]*storage.RedisAdapter),
		config:         config,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		replicationLog: &ReplicationLog{entries: make([]ReplicationEntry, 0)},
	}

	m.startSyncMonitor()

	return m
}

func (m *Manager) RegisterReplica(shardID int, node *ReplicaNode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.replicas[shardID]; !exists {
		m.replicas[shardID] = make([]*ReplicaNode, 0)
	}

	for _, existing := range m.replicas[shardID] {
		if existing.Address == node.Address {
			return fmt.Errorf("replica already registered: %s", node.Address)
		}
	}

	adapter, err := storage.NewRedisAdapter(storage.RedisConfig{
		Addr:     node.Address,
		Password: m.config.RedisPassword,
		DB:       m.config.RedisDB + shardID,
	}, m.logger)

	if err != nil {
		return fmt.Errorf("failed to create adapter for replica: %w", err)
	}

	m.adapters[node.Address] = adapter
	m.replicas[shardID] = append(m.replicas[shardID], node)

	if node.Role == "primary" {
		m.primaryNodes[shardID] = node.Address
	}

	m.logger.Info("Registered replica",
		zap.Int("shard", shardID),
		zap.String("address", node.Address),
		zap.String("role", node.Role))

	return nil
}

func (m *Manager) ReplicateInsert(ctx context.Context, indexName string, vector *storage.StoredVector, shardID int) error {
	m.mu.RLock()
	replicas := m.replicas[shardID]
	m.mu.RUnlock()

	if len(replicas) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errors := make([]error, 0)
	var errorMu sync.Mutex

	for _, replica := range replicas {
		if replica.Status != "active" || replica.Role == "primary" {
			continue
		}

		wg.Add(1)
		go func(r *ReplicaNode) {
			defer wg.Done()

			adapter, exists := m.adapters[r.Address]
			if !exists {
				errorMu.Lock()
				errors = append(errors, fmt.Errorf("adapter not found for replica: %s", r.Address))
				errorMu.Unlock()
				return
			}

			if err := adapter.StoreVector(ctx, indexName, vector); err != nil {
				errorMu.Lock()
				errors = append(errors, err)
				errorMu.Unlock()

				m.logReplication(shardID, "insert", vector.ID, false)
				m.markReplicaFailed(shardID, r.Address)
			} else {
				m.logReplication(shardID, "insert", vector.ID, true)
				m.updateReplicaSync(shardID, r.Address)
			}
		}(replica)
	}

	wg.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("replication failed on %d replicas", len(errors))
	}

	return nil
}

func (m *Manager) ReplicateBatch(ctx context.Context, indexName string, vectors []*storage.StoredVector, shardID int) error {
	m.mu.RLock()
	replicas := m.replicas[shardID]
	m.mu.RUnlock()

	if len(replicas) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, replica := range replicas {
		if replica.Status != "active" || replica.Role == "primary" {
			continue
		}

		wg.Add(1)
		go func(r *ReplicaNode) {
			defer wg.Done()

			adapter, exists := m.adapters[r.Address]
			if !exists {
				return
			}

			if err := adapter.BatchStoreVectors(ctx, indexName, vectors); err != nil {
				m.logger.Warn("Batch replication failed",
					zap.String("replica", r.Address),
					zap.Error(err))
				m.markReplicaFailed(shardID, r.Address)
			} else {
				m.updateReplicaSync(shardID, r.Address)
			}
		}(replica)
	}

	wg.Wait()
	return nil
}

func (m *Manager) ReplicateUpdate(ctx context.Context, indexName string, vector *storage.StoredVector, shardID int) error {
	return m.ReplicateInsert(ctx, indexName, vector, shardID)
}

func (m *Manager) ReplicateDelete(ctx context.Context, indexName, vectorID string, shardID int) error {
	m.mu.RLock()
	replicas := m.replicas[shardID]
	m.mu.RUnlock()

	if len(replicas) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, replica := range replicas {
		if replica.Status != "active" || replica.Role == "primary" {
			continue
		}

		wg.Add(1)
		go func(r *ReplicaNode) {
			defer wg.Done()

			adapter, exists := m.adapters[r.Address]
			if !exists {
				return
			}

			if err := adapter.DeleteVector(ctx, indexName, vectorID); err != nil {
				m.logger.Warn("Delete replication failed",
					zap.String("replica", r.Address),
					zap.Error(err))
				m.logReplication(shardID, "delete", vectorID, false)
			} else {
				m.logReplication(shardID, "delete", vectorID, true)
				m.updateReplicaSync(shardID, r.Address)
			}
		}(replica)
	}

	wg.Wait()
	return nil
}

func (m *Manager) ReplicateBatchDelete(ctx context.Context, indexName string, vectorIDs []string, shardID int) error {
	m.mu.RLock()
	replicas := m.replicas[shardID]
	m.mu.RUnlock()

	if len(replicas) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, replica := range replicas {
		if replica.Status != "active" || replica.Role == "primary" {
			continue
		}

		wg.Add(1)
		go func(r *ReplicaNode) {
			defer wg.Done()

			adapter, exists := m.adapters[r.Address]
			if !exists {
				return
			}

			if _, err := adapter.BatchDeleteVectors(ctx, indexName, vectorIDs); err != nil {
				m.logger.Warn("Batch delete replication failed",
					zap.String("replica", r.Address),
					zap.Error(err))
			} else {
				m.updateReplicaSync(shardID, r.Address)
			}
		}(replica)
	}

	wg.Wait()
	return nil
}

func (m *Manager) PromoteReplica(shardID int, replicaAddress string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	replicas, exists := m.replicas[shardID]
	if !exists {
		return fmt.Errorf("no replicas for shard %d", shardID)
	}

	var targetReplica *ReplicaNode
	for _, r := range replicas {
		if r.Address == replicaAddress {
			targetReplica = r
			break
		}
	}

	if targetReplica == nil {
		return fmt.Errorf("replica not found: %s", replicaAddress)
	}

	if targetReplica.Status != "active" {
		return fmt.Errorf("cannot promote inactive replica: %s", replicaAddress)
	}

	oldPrimary := m.primaryNodes[shardID]

	for _, r := range replicas {
		if r.Address == oldPrimary {
			r.Role = "secondary"
		} else if r.Address == replicaAddress {
			r.Role = "primary"
		}
	}

	m.primaryNodes[shardID] = replicaAddress

	m.logger.Info("Promoted replica to primary",
		zap.Int("shard", shardID),
		zap.String("new_primary", replicaAddress),
		zap.String("old_primary", oldPrimary))

	return nil
}

func (m *Manager) GetReplicaStatus(shardID int) ([]*ReplicaNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	replicas, exists := m.replicas[shardID]
	if !exists {
		return nil, fmt.Errorf("no replicas for shard %d", shardID)
	}

	result := make([]*ReplicaNode, len(replicas))
	for i, r := range replicas {
		replica := *r
		result[i] = &replica
	}

	return result, nil
}

func (m *Manager) startSyncMonitor() {
	m.syncTicker = time.NewTicker(m.config.SyncInterval)

	go func() {
		for {
			select {
			case <-m.syncTicker.C:
				m.checkReplicaHealth()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

func (m *Manager) checkReplicaHealth() {
	m.mu.RLock()
	allReplicas := make(map[int][]*ReplicaNode)
	for shardID, replicas := range m.replicas {
		allReplicas[shardID] = replicas
	}
	m.mu.RUnlock()

	for shardID, replicas := range allReplicas {
		for _, replica := range replicas {
			if replica.Role == "primary" {
				continue
			}

			lag := time.Since(replica.LastSync)
			if lag > m.config.MaxLag {
				m.logger.Warn("Replica lag exceeded threshold",
					zap.Int("shard", shardID),
					zap.String("replica", replica.Address),
					zap.Duration("lag", lag))

				if lag > m.config.FailoverTimeout {
					m.markReplicaFailed(shardID, replica.Address)
				}
			}
		}
	}
}

func (m *Manager) markReplicaFailed(shardID int, address string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	replicas, exists := m.replicas[shardID]
	if !exists {
		return
	}

	for _, r := range replicas {
		if r.Address == address {
			r.Status = "failed"
			m.logger.Error("Marked replica as failed",
				zap.Int("shard", shardID),
				zap.String("address", address))
			break
		}
	}
}

func (m *Manager) updateReplicaSync(shardID int, address string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	replicas, exists := m.replicas[shardID]
	if !exists {
		return
	}

	for _, r := range replicas {
		if r.Address == address {
			r.LastSync = time.Now()
			r.Lag = 0
			break
		}
	}
}

func (m *Manager) logReplication(shardID int, operation, key string, success bool) {
	m.replicationLog.mu.Lock()
	defer m.replicationLog.mu.Unlock()

	entry := ReplicationEntry{
		Timestamp: time.Now(),
		ShardID:   shardID,
		Operation: operation,
		Key:       key,
		Success:   success,
	}

	m.replicationLog.entries = append(m.replicationLog.entries, entry)

	if len(m.replicationLog.entries) > 10000 {
		m.replicationLog.entries = m.replicationLog.entries[5000:]
	}
}

func (m *Manager) GetReplicationLog(limit int) []ReplicationEntry {
	m.replicationLog.mu.RLock()
	defer m.replicationLog.mu.RUnlock()

	if limit <= 0 || limit > len(m.replicationLog.entries) {
		limit = len(m.replicationLog.entries)
	}

	result := make([]ReplicationEntry, limit)
	start := len(m.replicationLog.entries) - limit
	copy(result, m.replicationLog.entries[start:])

	return result
}

func (m *Manager) Close() error {
	m.cancel()

	if m.syncTicker != nil {
		m.syncTicker.Stop()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, adapter := range m.adapters {
		if err := adapter.Close(); err != nil {
			m.logger.Error("Failed to close adapter", zap.Error(err))
		}
	}

	return nil
}
