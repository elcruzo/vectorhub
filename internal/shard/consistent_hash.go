package shard

import (
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
)

type ConsistentHash struct {
	nodes       map[uint32]string
	sortedKeys  []uint32
	virtualNodes int
	mu          sync.RWMutex
}

func NewConsistentHash(virtualNodes int) *ConsistentHash {
	return &ConsistentHash{
		nodes:        make(map[uint32]string),
		sortedKeys:   make([]uint32, 0),
		virtualNodes: virtualNodes,
	}
}

func (c *ConsistentHash) hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

func (c *ConsistentHash) AddNode(node string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := 0; i < c.virtualNodes; i++ {
		virtualKey := fmt.Sprintf("%s#%d", node, i)
		hash := c.hash(virtualKey)
		c.nodes[hash] = node
		c.sortedKeys = append(c.sortedKeys, hash)
	}

	sort.Slice(c.sortedKeys, func(i, j int) bool {
		return c.sortedKeys[i] < c.sortedKeys[j]
	})
}

func (c *ConsistentHash) RemoveNode(node string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newSortedKeys := make([]uint32, 0)
	for i := 0; i < c.virtualNodes; i++ {
		virtualKey := fmt.Sprintf("%s#%d", node, i)
		hash := c.hash(virtualKey)
		delete(c.nodes, hash)
	}

	for _, key := range c.sortedKeys {
		if _, exists := c.nodes[key]; exists {
			newSortedKeys = append(newSortedKeys, key)
		}
	}

	c.sortedKeys = newSortedKeys
}

func (c *ConsistentHash) GetNode(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.nodes) == 0 {
		return "", fmt.Errorf("no nodes available")
	}

	hash := c.hash(key)
	idx := sort.Search(len(c.sortedKeys), func(i int) bool {
		return c.sortedKeys[i] >= hash
	})

	if idx == len(c.sortedKeys) {
		idx = 0
	}

	return c.nodes[c.sortedKeys[idx]], nil
}

func (c *ConsistentHash) GetNodes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeMap := make(map[string]bool)
	for _, node := range c.nodes {
		nodeMap[node] = true
	}

	nodes := make([]string, 0, len(nodeMap))
	for node := range nodeMap {
		nodes = append(nodes, node)
	}

	return nodes
}

func (c *ConsistentHash) GetNodeCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeMap := make(map[string]bool)
	for _, node := range c.nodes {
		nodeMap[node] = true
	}

	return len(nodeMap)
}