package state

import (
	"context"
	"sync"

	tfjson "github.com/hashicorp/terraform-json"
)

// CachedStateReader wraps a StateReader and caches results per layer path,
// avoiding repeated state reads for the same layer within a single run.
type CachedStateReader struct {
	inner StateReader
	mu    sync.Mutex
	cache map[string]*tfjson.State
}

// NewCachedStateReader creates a caching wrapper around an existing StateReader.
func NewCachedStateReader(inner StateReader) *CachedStateReader {
	return &CachedStateReader{
		inner: inner,
		cache: make(map[string]*tfjson.State),
	}
}

// ReadState returns the state for the given layer, reading it from the inner
// reader on first call and returning the cached result on subsequent calls.
func (c *CachedStateReader) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	c.mu.Lock()
	if s, ok := c.cache[layerPath]; ok {
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()

	s, err := c.inner.ReadState(ctx, layerPath)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[layerPath] = s
	c.mu.Unlock()

	return s, nil
}
