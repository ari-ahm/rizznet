package engine

import (
	"container/heap"
	"rizznet/internal/model"
)

// ScoredProxy is a wrapper for the heap
type ScoredProxy struct {
	Proxy model.Proxy
	Score float64
	Index int
}

// A PriorityQueue implements heap.Interface and holds ScoredProxies.
type PriorityQueue []*ScoredProxy

func (pq PriorityQueue) Len() int { return len(pq) }

// Less determines the order. We want a Min-Heap, so Less means "Lower Score".
// The item at index 0 will be the one with the lowest score.
func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].Score < pq[j].Score
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*ScoredProxy)
	item.Index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.Index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// Bucket wraps the heap with high-level logic (Push/Pop/Evict).
type Bucket struct {
	Capacity int
	pq       PriorityQueue
}

func NewBucket(capacity int) *Bucket {
	b := &Bucket{
		Capacity: capacity,
		pq:       make(PriorityQueue, 0, capacity),
	}
	heap.Init(&b.pq)
	return b
}

// Offer tries to add a proxy to the bucket.
// Returns true if added (and potentially evicted someone), false if rejected.
func (b *Bucket) Offer(proxy model.Proxy, score float64) bool {
	if score <= 0 {
		return false
	}

	// 1. Bucket is not full -> Add it
	if b.pq.Len() < b.Capacity {
		heap.Push(&b.pq, &ScoredProxy{Proxy: proxy, Score: score})
		return true
	}

	// 2. Bucket is full -> Compare with Worst (Root)
	worst := b.pq[0]
	if score > worst.Score {
		// New proxy is better! Kick out the worst.
		heap.Pop(&b.pq)
		heap.Push(&b.pq, &ScoredProxy{Proxy: proxy, Score: score})
		return true
	}

	// New proxy is worse than the worst in the bucket. Reject.
	return false
}

// GetProxies returns the survivors
func (b *Bucket) GetProxies() []model.Proxy {
	res := make([]model.Proxy, len(b.pq))
	for i, item := range b.pq {
		res[i] = item.Proxy
	}
	return res
}
