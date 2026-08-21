// Package redis provides a Redis implementation of the faction.Cache
// interface for use with the cosfaction disposition engine.
//
// The caller is responsible for establishing and managing the redis.Client.
// This adapter performs no connection management of its own.
//
// Usage:
//
//	rdb := redis.NewClient(&redis.Options{
//	    Addr: os.Getenv("REDIS_ADDR"),
//	})
//
//	cache := redisadapter.New(rdb, redisadapter.Config{
//	    TTL: 24 * time.Hour,
//	})
//
//	engine, err := faction.New(faction.Config{
//	    Store: store,
//	    Cache: cache,
//	    // ...
//	})
//
// Storage layout:
//
// Each entity's dispositions are stored as a Redis Hash:
//
//	Key:   faction:dispositions:{entityID}
//	Field: {factionID}
//	Value: {value}:{unix_nanoseconds}
//
// This layout allows O(1) single-faction reads and O(N) full-entity reads
// in a single round trip, with no JSON serialization overhead.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	faction "github.com/d1sco/cosfaction"
	"github.com/redis/go-redis/v9"
)

const (
	// keyPrefix namespaces all cosfaction keys in Redis.
	keyPrefix = "faction:dispositions:"

	// defaultTTL is how long a disposition hash lives in Redis without
	// being touched. The store remains authoritative — eviction just means
	// the next read falls through to PostgreSQL.
	defaultTTL = 24 * time.Hour
)

// Config controls the behaviour of the Redis cache adapter.
type Config struct {
	// TTL is how long a cached disposition hash lives without being accessed.
	// Each write resets the TTL. Zero uses the default of 24 hours.
	TTL time.Duration

	// KeyPrefix overrides the default "faction:dispositions:" namespace.
	// Useful when multiple services share a Redis instance.
	KeyPrefix string
}

// Cache implements faction.Cache against a Redis instance.
// All methods are safe for concurrent use.
type Cache struct {
	rdb    *redis.Client
	ttl    time.Duration
	prefix string
}

// New constructs a Cache from an existing redis.Client.
func New(rdb *redis.Client, cfg Config) *Cache {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = keyPrefix
	}

	return &Cache{
		rdb:    rdb,
		ttl:    ttl,
		prefix: prefix,
	}
}

// GetDisposition retrieves a cached stored disposition for an entity with
// a specific faction. Returns the value, whether it was found, and any error.
func (c *Cache) GetDisposition(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
) (faction.StoredDisposition, bool, error) {
	key := c.key(entityID)

	raw, err := c.rdb.HGet(ctx, key, string(factionID)).Result()
	if errors.Is(err, redis.Nil) {
		return faction.StoredDisposition{}, false, nil
	}
	if err != nil {
		return faction.StoredDisposition{}, false, fmt.Errorf(
			"cosfaction redis: GetDisposition %s/%s: %w",
			entityID, factionID, err,
		)
	}

	stored, err := decode(factionID, raw)
	if err != nil {
		return faction.StoredDisposition{}, false, fmt.Errorf(
			"cosfaction redis: GetDisposition decode %s/%s: %w",
			entityID, factionID, err,
		)
	}

	return stored, true, nil
}

// SetDisposition writes a stored disposition to the cache and resets the TTL.
func (c *Cache) SetDisposition(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
	stored faction.StoredDisposition,
) error {
	key := c.key(entityID)

	pipe := c.rdb.Pipeline()
	pipe.HSet(ctx, key, string(factionID), encode(stored))
	pipe.Expire(ctx, key, c.ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf(
			"cosfaction redis: SetDisposition %s/%s: %w",
			entityID, factionID, err,
		)
	}
	return nil
}

// Invalidate removes a single cached faction disposition for an entity,
// forcing the next read to fall through to the Store.
func (c *Cache) Invalidate(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
) error {
	key := c.key(entityID)

	err := c.rdb.HDel(ctx, key, string(factionID)).Err()
	if err != nil {
		return fmt.Errorf(
			"cosfaction redis: Invalidate %s/%s: %w",
			entityID, factionID, err,
		)
	}
	return nil
}

// InvalidateAll removes all cached dispositions for an entity by deleting
// the entire hash key.
func (c *Cache) InvalidateAll(
	ctx context.Context,
	entityID faction.EntityID,
) error {
	key := c.key(entityID)

	err := c.rdb.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf(
			"cosfaction redis: InvalidateAll %s: %w",
			entityID, err,
		)
	}
	return nil
}

// key builds the Redis hash key for an entity's dispositions.
func (c *Cache) key(entityID faction.EntityID) string {
	return c.prefix + string(entityID)
}

// encode serializes a StoredDisposition to a compact string for storage
// as a Redis hash field value.
//
// Format: "{value}:{unix_nanoseconds}"
//
// Example: "250:1722470400000000000"
func encode(stored faction.StoredDisposition) string {
	return fmt.Sprintf("%d:%d",
		stored.Value,
		stored.UpdatedAt.UnixNano(),
	)
}

// decode deserializes a Redis hash field value back to a StoredDisposition.
func decode(factionID faction.FactionID, raw string) (faction.StoredDisposition, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return faction.StoredDisposition{}, fmt.Errorf(
			"invalid encoded disposition %q: expected value:nanoseconds",
			raw,
		)
	}

	value, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return faction.StoredDisposition{}, fmt.Errorf(
			"invalid value in encoded disposition %q: %w",
			raw, err,
		)
	}

	nanos, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return faction.StoredDisposition{}, fmt.Errorf(
			"invalid timestamp in encoded disposition %q: %w",
			raw, err,
		)
	}

	return faction.StoredDisposition{
		FactionID: factionID,
		Value:     value,
		UpdatedAt: time.Unix(0, nanos),
	}, nil
}
