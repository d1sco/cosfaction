package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	faction "github.com/cosfaction/cosfaction"
	postgres "github.com/cosfaction/cosfaction/adapters/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// requireDB returns a database pool for integration testing.
// Tests are skipped if DATABASE_URL is not set or if -short is passed.
//
// To run integration tests:
//
//	DATABASE_URL=postgres://user:pass@localhost:5432/testdb go test ./...
//
// With Docker:
//
//	docker run --rm -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16
//	DATABASE_URL=postgres://postgres:test@localhost:5432/postgres go test ./...
func requireDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test: DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Clean up test data after each test.
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM faction_disposition_history WHERE entity_id LIKE 'test-%'")
		pool.Exec(ctx, "DELETE FROM faction_dispositions WHERE entity_id LIKE 'test-%'")
	})

	return pool
}

func TestStore_GetDisposition_NotFound(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	stored, err := store.GetDisposition(ctx, "test-entity-1", "iar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored.Value != 0 {
		t.Errorf("expected zero value for missing record, got %d", stored.Value)
	}
	if !stored.UpdatedAt.IsZero() {
		t.Errorf("expected zero timestamp for missing record, got %v", stored.UpdatedAt)
	}
}

func TestStore_SetAndGetDisposition(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-2")
	factionID := faction.FactionID("iar")

	if err := store.SetDisposition(ctx, entityID, factionID, 250); err != nil {
		t.Fatalf("SetDisposition failed: %v", err)
	}

	stored, err := store.GetDisposition(ctx, entityID, factionID)
	if err != nil {
		t.Fatalf("GetDisposition failed: %v", err)
	}

	if stored.Value != 250 {
		t.Errorf("expected value 250, got %d", stored.Value)
	}
	if stored.FactionID != factionID {
		t.Errorf("expected factionID %s, got %s", factionID, stored.FactionID)
	}
	if stored.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt after SetDisposition")
	}
	if time.Since(stored.UpdatedAt) > 5*time.Second {
		t.Errorf("UpdatedAt is too old: %v", stored.UpdatedAt)
	}
}

func TestStore_SetDisposition_Upsert(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-3")
	factionID := faction.FactionID("union")

	if err := store.SetDisposition(ctx, entityID, factionID, 100); err != nil {
		t.Fatalf("first SetDisposition failed: %v", err)
	}

	if err := store.SetDisposition(ctx, entityID, factionID, 350); err != nil {
		t.Fatalf("second SetDisposition failed: %v", err)
	}

	stored, err := store.GetDisposition(ctx, entityID, factionID)
	if err != nil {
		t.Fatalf("GetDisposition failed: %v", err)
	}

	if stored.Value != 350 {
		t.Errorf("expected upserted value 350, got %d", stored.Value)
	}
}

func TestStore_GetAllDispositions(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-4")

	if err := store.SetDisposition(ctx, entityID, "iar", 200); err != nil {
		t.Fatalf("SetDisposition iar failed: %v", err)
	}
	if err := store.SetDisposition(ctx, entityID, "union", -150); err != nil {
		t.Fatalf("SetDisposition union failed: %v", err)
	}

	all, err := store.GetAllDispositions(ctx, entityID)
	if err != nil {
		t.Fatalf("GetAllDispositions failed: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 dispositions, got %d", len(all))
	}

	byFaction := make(map[faction.FactionID]faction.StoredDisposition)
	for _, s := range all {
		byFaction[s.FactionID] = s
	}

	if byFaction["iar"].Value != 200 {
		t.Errorf("expected iar=200, got %d", byFaction["iar"].Value)
	}
	if byFaction["union"].Value != -150 {
		t.Errorf("expected union=-150, got %d", byFaction["union"].Value)
	}
}

func TestStore_GetAllDispositions_Empty(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	all, err := store.GetAllDispositions(ctx, "test-entity-no-dispositions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty slice, got %d records", len(all))
	}
}

func TestStore_RecordAndGetHistory(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-5")
	factionID := faction.FactionID("iar")

	records := []faction.DispositionRecord{
		{
			EntityID:         entityID,
			FactionID:        factionID,
			PreviousValue:    0,
			NewValue:         100,
			Delta:            100,
			PropagationOrder: 1,
			Reason:           "quest completed",
			Source:           "faction_quest",
			OccurredAt:       time.Now().Add(-2 * time.Minute).Unix(),
		},
		{
			EntityID:         entityID,
			FactionID:        factionID,
			PreviousValue:    100,
			NewValue:         -40,
			Delta:            -140,
			PropagationOrder: 2,
			Reason:           "propagated from union action",
			Source:           "smuggler_delivery_quest",
			OccurredAt:       time.Now().Add(-1 * time.Minute).Unix(),
		},
	}

	for _, r := range records {
		if err := store.RecordDispositionChange(ctx, r); err != nil {
			t.Fatalf("RecordDispositionChange failed: %v", err)
		}
	}

	history, err := store.GetDispositionHistory(ctx, entityID, factionID, 10)
	if err != nil {
		t.Fatalf("GetDispositionHistory failed: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history))
	}

	// History is ordered newest first.
	if history[0].Delta != -140 {
		t.Errorf("expected most recent delta -140, got %d", history[0].Delta)
	}
	if history[0].Reason != "propagated from union action" {
		t.Errorf("unexpected reason: %s", history[0].Reason)
	}
	if history[0].PropagationOrder != 2 {
		t.Errorf("expected propagation order 2, got %d", history[0].PropagationOrder)
	}
}

func TestStore_RecordHistory_NullableFields(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-6")
	factionID := faction.FactionID("iar")

	// Empty reason and source should write NULL not empty string.
	record := faction.DispositionRecord{
		EntityID:         entityID,
		FactionID:        factionID,
		PreviousValue:    0,
		NewValue:         50,
		Delta:            50,
		PropagationOrder: 1,
		Reason:           "",
		Source:           "",
		OccurredAt:       time.Now().Unix(),
	}

	if err := store.RecordDispositionChange(ctx, record); err != nil {
		t.Fatalf("RecordDispositionChange failed: %v", err)
	}

	history, err := store.GetDispositionHistory(ctx, entityID, factionID, 1)
	if err != nil {
		t.Fatalf("GetDispositionHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 record, got %d", len(history))
	}

	if history[0].Reason != "" {
		t.Errorf("expected empty reason, got %q", history[0].Reason)
	}
	if history[0].Source != "" {
		t.Errorf("expected empty source, got %q", history[0].Source)
	}
}

func TestStore_GetDispositionHistory_Limit(t *testing.T) {
	pool := requireDB(t)
	store := postgres.New(pool)
	ctx := context.Background()

	entityID := faction.EntityID("test-entity-7")
	factionID := faction.FactionID("union")

	for i := 0; i < 5; i++ {
		record := faction.DispositionRecord{
			EntityID:      entityID,
			FactionID:     factionID,
			PreviousValue: int64(i * 10),
			NewValue:      int64((i + 1) * 10),
			Delta:         10,
			OccurredAt:    time.Now().Add(time.Duration(i) * time.Second).Unix(),
		}
		if err := store.RecordDispositionChange(ctx, record); err != nil {
			t.Fatalf("RecordDispositionChange %d failed: %v", i, err)
		}
	}

	history, err := store.GetDispositionHistory(ctx, entityID, factionID, 3)
	if err != nil {
		t.Fatalf("GetDispositionHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Errorf("expected 3 records with limit=3, got %d", len(history))
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	pool := requireDB(t)
	ctx := context.Background()

	// Calling Migrate twice should not error.
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Errorf("second Migrate call failed: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Errorf("third Migrate call failed: %v", err)
	}
}
