package render

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image/png"
	"sync"
)

// Cache renders models on demand and keeps the encoded PNGs for the lifetime of
// the process, until an import replaces the assets they came from.
//
// A render costs a few milliseconds and its inputs — model JSON and textures —
// only change on import, so caching in memory is enough and keeps the asset tree
// free of generated files. Each entry is small (a 32px icon is a few hundred
// bytes) and the set is bounded by how many models the catalog has.
//
// Concurrent requests for the same icon wait on one render rather than each
// doing the work: a page opening thirty pipe icons at once is the normal case.
type Cache struct {
	loader  *Loader
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	done sync.WaitGroup
	png  []byte
	etag string
	err  error
}

func NewCache(loader *Loader) *Cache {
	return &Cache{loader: loader, entries: map[string]*entry{}}
}

// Get returns the PNG for a model at a given size, plus an ETag derived from the
// bytes so a client can revalidate cheaply.
func (c *Cache) Get(ref string, size int) (data []byte, etag string, err error) {
	if err := SanitizeRef(ref); err != nil {
		return nil, "", err
	}
	key := fmt.Sprintf("%s@%d", ref, size)

	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.mu.Unlock()
		e.done.Wait()
		return e.png, e.etag, e.err
	}
	e := &entry{}
	e.done.Add(1)
	c.entries[key] = e
	c.mu.Unlock()

	// Released before anything else can fail. A panic escaping the render would
	// otherwise leave the entry in the map with its WaitGroup never released,
	// and every later request for that key would block forever rather than just
	// this one failing.
	defer func() {
		if r := recover(); r != nil {
			e.err = fmt.Errorf("render: %q panicked: %v", ref, r)
		}
		e.done.Done()
		if e.err != nil {
			// A failure must not be cached: the model may simply not be
			// imported yet, and the next request should try again.
			c.mu.Lock()
			delete(c.entries, key)
			c.mu.Unlock()
		}
	}()

	e.png, e.etag, e.err = c.render(ref, size)
	return e.png, e.etag, e.err
}

func (c *Cache) render(ref string, size int) ([]byte, string, error) {
	scene, err := c.loader.LoadScene(ref)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, Render(scene, size)); err != nil {
		return nil, "", fmt.Errorf("render: encode %q: %w", ref, err)
	}
	sum := sha1.Sum(buf.Bytes())
	return buf.Bytes(), `"` + hex.EncodeToString(sum[:]) + `"`, nil
}

// Invalidate drops everything, for use after an import replaces assets.
//
// A render already in flight keeps its own entry and finishes normally; it
// simply lands in the discarded map and is re-rendered on the next request.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.entries = map[string]*entry{}
	c.mu.Unlock()
}
