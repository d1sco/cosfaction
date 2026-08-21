package redis_test

import (
	"context"
	"os"
	"testing"
	"time"

	faction "github.com/d1sco/cosfaction"
	redisadapter "github.com/d1sco/cosfaction/adapters/redis"
	"github.com/redis/go-redis/v9"
)

// requireRedis returns a Redis client for integration testing.
// Tests are skipped if REDIS_ADDR is not set or if -short is passed.
//
// To run integration tests:
//
//	REDIS_ADDR=localhost:6379 go test ./...
//
// With Docker:
//
//	docker run --rm -p 6379:6379 redis:7
//	REDIS_ADDR=localhost:6379 go test ./...
func requireRedis(t *testing.T) *redis.Client {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("skipping integration test: REDIS_ADDR not set")
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to connect to Redis: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test keys.
		keys, _ := rdb.Keys(ctx, "faction:dispositions:test-*").Result()
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
		}
		rdb.Close()
	})

	return rdb
}

func TestCache_GetDisposition_Miss(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	stored, found, err := cache.GetDisposition(ctx, "test-entity-1", "iar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected cache miss, got hit")
	}
	if stored.Value != 0 {
		t.Errorf("expected zero value on miss, got %d", stored.Value)
	}
}

func TestCache_SetAndGet(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-2")
	factionID := faction.FactionID("iar")
	now := time.Now().Truncate(time.Nanosecond)

	stored := faction.StoredDisposition{
		FactionID: factionID,
		Value:     250,
		UpdatedAt: now,
	}

	if err := cache.SetDisposition(ctx, entityID, factionID, stored); err != nil {
		t.Fatalf("SetDisposition failed: %v", err)
	}

	got, found, err := cache.GetDisposition(ctx, entityID, factionID)
	if err != nil {
		t.Fatalf("GetDisposition failed: %v", err)
	}
	if !found {
		t.Fatal("expected cache hit, got miss")
	}
	if got.Value != 250 {
		t.Errorf("expected value 250, got %d", got.Value)
	}
	if got.FactionID != factionID {
		t.Errorf("expected factionID %s, got %s", factionID, got.FactionID)
	}
	// Timestamps are encoded as nanoseconds so should round-trip exactly.
	if !got.UpdatedAt.Equal(now) {
		t.Errorf("expected UpdatedAt %v, got %v", now, got.UpdatedAt)
	}
}

func TestCache_SetDisposition_OverwritesExisting(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-3")
	factionID := faction.FactionID("union")

	first := faction.StoredDisposition{FactionID: factionID, Value: 100, UpdatedAt: time.Now()}
	if err := cache.SetDisposition(ctx, entityID, factionID, first); err != nil {
		t.Fatalf("first SetDisposition failed: %v", err)
	}

	second := faction.StoredDisposition{FactionID: factionID, Value: 350, UpdatedAt: time.Now()}
	if err := cache.SetDisposition(ctx, entityID, factionID, second); err != nil {
		t.Fatalf("second SetDisposition failed: %v", err)
	}

	got, found, err := cache.GetDisposition(ctx, entityID, factionID)
	if err != nil {
		t.Fatalf("GetDisposition failed: %v", err)
	}
	if !found {
		t.Fatal("expected cache hit")
	}
	if got.Value != 350 {
		t.Errorf("expected overwritten value 350, got %d", got.Value)
	}
}

func TestCache_MultipleFactions_SameEntity(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-4")

	iar := faction.StoredDisposition{FactionID: "iar", Value: 200, UpdatedAt: time.Now()}
	union := faction.StoredDisposition{FactionID: "union", Value: -150, UpdatedAt: time.Now()}

	if err := cache.SetDisposition(ctx, entityID, "iar", iar); err != nil {
		t.Fatalf("SetDisposition iar failed: %v", err)
	}
	if err := cache.SetDisposition(ctx, entityID, "union", union); err != nil {
		t.Fatalf("SetDisposition union failed: %v", err)
	}

	gotIAR, found, err := cache.GetDisposition(ctx, entityID, "iar")
	if err != nil || !found {
		t.Fatalf("GetDisposition iar failed: err=%v found=%v", err, found)
	}
	if gotIAR.Value != 200 {
		t.Errorf("expected iar=200, got %d", gotIAR.Value)
	}

	gotUnion, found, err := cache.GetDisposition(ctx, entityID, "union")
	if err != nil || !found {
		t.Fatalf("GetDisposition union failed: err=%v found=%v", err, found)
	}
	if gotUnion.Value != -150 {
		t.Errorf("expected union=-150, got %d", gotUnion.Value)
	}
}

func TestCache_Invalidate_SingleFaction(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-5")

	iar := faction.StoredDisposition{FactionID: "iar", Value: 200, UpdatedAt: time.Now()}
	union := faction.StoredDisposition{FactionID: "union", Value: 100, UpdatedAt: time.Now()}

	cache.SetDisposition(ctx, entityID, "iar", iar)
	cache.SetDisposition(ctx, entityID, "union", union)

	// Invalidate only IAR.
	if err := cache.Invalidate(ctx, entityID, "iar"); err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}

	_, found, err := cache.GetDisposition(ctx, entityID, "iar")
	if err != nil {
		t.Fatalf("GetDisposition after invalidate failed: %v", err)
	}
	if found {
		t.Error("expected cache miss after Invalidate, got hit")
	}

	// Union should still be cached.
	gotUnion, found, err := cache.GetDisposition(ctx, entityID, "union")
	if err != nil || !found {
		t.Fatalf("GetDisposition union after partial invalidate: err=%v found=%v", err, found)
	}
	if gotUnion.Value != 100 {
		t.Errorf("expected union=100 after partial invalidate, got %d", gotUnion.Value)
	}
}

func TestCache_InvalidateAll(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-6")

	cache.SetDisposition(ctx, entityID, "iar", faction.StoredDisposition{FactionID: "iar", Value: 200, UpdatedAt: time.Now()})
	cache.SetDisposition(ctx, entityID, "union", faction.StoredDisposition{FactionID: "union", Value: 100, UpdatedAt: time.Now()})

	if err := cache.InvalidateAll(ctx, entityID); err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	for _, fid := range []faction.FactionID{"iar", "union"} {
		_, found, err := cache.GetDisposition(ctx, entityID, fid)
		if err != nil {
			t.Fatalf("GetDisposition %s after InvalidateAll: %v", fid, err)
		}
		if found {
			t.Errorf("expected cache miss for %s after InvalidateAll, got hit", fid)
		}
	}
}

func TestCache_NegativeValues(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{TTL: time.Minute})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-7")
	factionID := faction.FactionID("iar")

	stored := faction.StoredDisposition{
		FactionID: factionID,
		Value:     -750,
		UpdatedAt: time.Now(),
	}

	cache.SetDisposition(ctx, entityID, factionID, stored)

	got, found, err := cache.GetDisposition(ctx, entityID, factionID)
	if err != nil || !found {
		t.Fatalf("GetDisposition failed: err=%v found=%v", err, found)
	}
	if got.Value != -750 {
		t.Errorf("expected -750, got %d", got.Value)
	}
}

func TestCache_CustomKeyPrefix(t *testing.T) {
	rdb := requireRedis(t)
	cache := redisadapter.New(rdb, redisadapter.Config{
		TTL:       time.Minute,
		KeyPrefix: "mygame:factions:",
	})
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-8")
	factionID := faction.FactionID("iar")

	stored := faction.StoredDisposition{FactionID: factionID, Value: 100, UpdatedAt: time.Now()}
	if err := cache.SetDisposition(ctx, entityID, factionID, stored); err != nil {
		t.Fatalf("SetDisposition failed: %v", err)
	}

	// Key should be under custom prefix.
	exists, err := rdb.Exists(ctx, "mygame:factions:test-entity-8").Result()
	if err != nil {
		t.Fatalf("Exists check failed: %v", err)
	}
	if exists != 1 {
		t.Error("expected key under custom prefix to exist")
	}

	// Should not exist under default prefix.
	exists, err = rdb.Exists(ctx, "faction:dispositions:test-entity-8").Result()
	if err != nil {
		t.Fatalf("Exists check failed: %v", err)
	}
	if exists != 0 {
		t.Error("expected no key under default prefix when custom prefix is set")
	}

	// Cleanup custom prefix key.
	t.Cleanup(func() {
		rdb.Del(ctx, "mygame:factions:test-entity-8")
	})
}
