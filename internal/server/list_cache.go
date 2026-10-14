package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"nailist/internal/logging"
)

// Public snapshots are immutable, encoded once, and valid until a successful write.
// A generation prevents an in-flight query from restoring invalidated data.
type listSnapshot struct {
	body  []byte
	count int
}

type listCache struct {
	mu         sync.Mutex
	generation uint64
	valid      bool
	snapshot   listSnapshot
	flight     chan struct{}
}

func encodeList[T any](rows []T, err error) (listSnapshot, error) {
	if err != nil {
		return listSnapshot{}, err
	}
	if rows == nil {
		rows = []T{}
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return listSnapshot{}, err
	}
	return listSnapshot{body: append(body, '\n'), count: len(rows)}, nil
}

func (c *listCache) get(ctx context.Context, resource string, load func(context.Context) (listSnapshot, error)) (listSnapshot, error) {
	for {
		if err := ctx.Err(); err != nil {
			return listSnapshot{}, err
		}
		c.mu.Lock()
		if c.valid {
			snapshot := c.snapshot
			c.mu.Unlock()
			logging.FromContext(ctx).DebugContext(ctx, "公开列表命中内存缓存，无需查询数据库或重新编码", "component", "cache", "event", "lists.cache.hit", "resource", resource, "count", snapshot.count)
			return snapshot, nil
		}
		if c.flight != nil {
			flight := c.flight
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return listSnapshot{}, ctx.Err()
			case <-flight:
				continue
			}
		}
		generation := c.generation
		flight := make(chan struct{})
		c.flight = flight
		c.mu.Unlock()

		var err error
		func() {
			var snapshot listSnapshot
			loaded := false
			// Always wake waiters, including after cancellation, query errors or panic.
			defer func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if loaded && err == nil && ctx.Err() == nil && c.generation == generation {
					c.snapshot, c.valid = snapshot, true
				}
				close(flight)
				c.flight = nil
			}()
			logging.FromContext(ctx).DebugContext(ctx, "公开列表缓存未建立或已失效，查询数据库并重建快照", "component", "cache", "event", "lists.cache.load", "resource", resource, "generation", generation)
			snapshot, err = load(ctx)
			loaded = true
		}()
		if err != nil {
			return listSnapshot{}, err
		}
		// If a write committed during loading, discard the old result and reload.
	}
}

func (c *listCache) invalidate(ctx context.Context, resource string) {
	c.mu.Lock()
	c.generation++
	c.valid, c.snapshot = false, listSnapshot{}
	generation := c.generation
	c.mu.Unlock()
	logging.FromContext(ctx).DebugContext(ctx, "列表变更已提交，内存缓存已失效，下次公开读取重新查询数据库", "component", "cache", "event", "lists.cache.invalidated", "resource", resource, "generation", generation)
}

func respondList(w http.ResponseWriter, r *http.Request, snapshot listSnapshot) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(snapshot.body); err != nil {
		responseFailure(r, http.StatusOK, err)
	}
}
