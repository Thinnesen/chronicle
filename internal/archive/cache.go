package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// CacheEntry is the saved result of a node of a template with memoize.
type CacheEntry struct {
	Outputs  *v1alpha1.Outputs
	Workflow string
	SavedAt  time.Time
}

// LoadCache returns the saved results of keys in a namespace. A key without a result is not in the map.
func (s *Store) LoadCache(ctx context.Context, namespace string, keys []string) (map[string]CacheEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, outputs, workflow, saved_at FROM step_cache WHERE namespace = $1 AND key = ANY($2)`,
		namespace, keys)
	if err != nil {
		return nil, fmt.Errorf("read the cache: %w", err)
	}
	defer rows.Close()
	out := map[string]CacheEntry{}
	for rows.Next() {
		var key string
		var outputs []byte
		var e CacheEntry
		if err := rows.Scan(&key, &outputs, &e.Workflow, &e.SavedAt); err != nil {
			return nil, fmt.Errorf("read the cache: %w", err)
		}
		if err := json.Unmarshal(outputs, &e.Outputs); err != nil {
			return nil, fmt.Errorf("read the cache entry %q: %w", key, err)
		}
		out[key] = e
	}
	return out, rows.Err()
}

// SaveCache saves the outputs of a node under a key. It replaces an older result.
func (s *Store) SaveCache(ctx context.Context, namespace, key, workflow, node string, outputs *v1alpha1.Outputs) error {
	data, err := json.Marshal(outputs)
	if err != nil {
		return fmt.Errorf("encode the outputs: %w", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO step_cache (namespace, key, outputs, workflow, node) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (namespace, key) DO UPDATE SET outputs = excluded.outputs, workflow = excluded.workflow, node = excluded.node, saved_at = now()`,
		namespace, key, data, workflow, node)
	if err != nil {
		return fmt.Errorf("save the cache entry: %w", err)
	}
	return nil
}
