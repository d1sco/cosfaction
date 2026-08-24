# cosfaction

Faction disposition engine for multiplayer games.

`cosfaction` models multi-axis faction relationships as a weighted directed graph and uses **Higher-Order Taylor Expansion** to propagate disposition changes across competing factions with mathematically guaranteed convergence.

## Why not just store a reputation integer?

A reputation integer tells you how much a faction likes a player.

`cosfaction` tells you how a player's relationship with one faction changes every other faction's opinion of them — and by exactly how much — with the political ripple effects that real competing factions would produce.

When a Smuggler defies an IAR inspector, the Union notices. When a Bounty Hunter delivers a Union operative to the IAR, the Union treats it as a betrayal. These effects emerge automatically from the faction relationship graph. No special casing. No hardcoded faction logic.

## Definitions

These terms have precise meanings in cosfaction that map directly to game design concepts.

**Disposition** — A signed numeric score representing how a faction feels about an entity at a point in time. Not a label. Not a binary. A number on a continuous scale that accumulates from every action the entity has ever taken that the faction can observe or infer.

**Delta** — A single disposition change event. The thing that happened — a quest completed, an inspection failed, a city alignment switched. Every delta has a magnitude (how significant) and a source faction (who is directly affected). The engine determines which other factions learn about it and how much.

**Tier** — A named band on the disposition scale. Tiers give human-readable meaning to ranges of raw values. A disposition of -340 means nothing to a player. *Wanted* means everything. Tiers are purely presentational — the engine operates on raw values and maps to tier names only for events and display.

**Relation** — A directed political connection between two factions with a signed weight. The weight encodes the political reality: are these factions allies, enemies, or indifferent? Relations are one-directional by design — the IAR's opinion of the Union does not have to mirror the Union's opinion of the IAR.

**Influence** — The weight of a Relation in the range `[-1.0, 1.0]`. This is the political verb. Negative influence means *opposition* — gaining standing with one faction costs standing with the other. Positive influence means *alliance* — gaining standing with one faction earns standing with the other. Zero means *indifference* — the factions do not pay attention to each other's relationships.

**Propagation** — The automatic spread of a disposition change beyond its direct target. When a player helps the Union, the IAR finds out — not because game code explicitly tells the IAR, but because the Relation between Union and IAR has negative influence and the Taylor expansion carries the signal through the graph. Propagation is what makes factions feel like political entities rather than independent stat bars.

**Order** — Which hop in the propagation chain produced a given effect. Order 1 is the direct effect on the target faction. Order 2 is the effect on factions directly related to the target. Order 3 is the effect on factions related to those. Each successive order is smaller due to double-guaranteed convergence — the signal attenuates as it travels.

**Convergence** — The mathematical guarantee that propagation always terminates with finite total effect. Two mechanisms enforce this simultaneously: normalized delta raised to the nth power shrinks geometrically, and factorial damping (n!) grows faster than any polynomial. A disposition delta of any magnitude will always produce a bounded total effect across the graph.

**Decay** — The gradual movement of disposition toward a target value over real time when no new deltas are applied. Decay is evaluated lazily on read rather than by a background process — the engine computes elapsed time since the last write and applies the rate at the moment of access. A player cannot freeze their standing in a favorable state by logging out.


## How it works

The delta is first normalized against the full disposition range to produce a dimensionless value `δ_n ∈ (-1, 1)`. The nth order term for faction `f` is then:

```
T_n(f) = (δ_n^n / n!) × Σ R(f₀,g₁) × R(g₁,g₂) × ... × R(g_{n-1}, f) × range
```

Where:
- `δ_n = delta / range` is the normalized delta
- `R(a,b)` is the influence weight of the relationship from faction `a` to faction `b`
- `range` is the full disposition scale span derived from your tier configuration
- `n!` is factorial damping

Normalization ensures `|δ_n| < 1`, so `δ_n^n` shrinks geometrically with order. Combined with factorial damping this produces **double-guaranteed convergence**: two independent mechanisms both drive higher order terms toward zero.

This has a meaningful game design consequence. Routine actions (small delta relative to range) produce negligible higher order effects. Major events (large delta relative to range) produce genuine political ripples across the entire faction graph. The mathematics encode political significance automatically.


## Quick start

```bash
go get github.com/d1sco/cosfaction@v0.1.1
```

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"

    faction "github.com/d1sco/cosfaction"
)

func main() {
    ctx := context.Background()

    // Build tiers without calculating boundaries manually.
    // Add or remove tier names — boundaries recalculate automatically.
    tiers, err := faction.EvenTiers(
        -10000, // dispositionMin
        10000,  // dispositionMax
        "Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Celebrated",
    )
    if err != nil {
        panic(err)
    }

    engine, err := faction.New(faction.Config{
        Factions: []faction.Faction{
            {ID: "iar",   Name: "Interstellar Authority Republic", Type: faction.FactionTypeGoverning},
            {ID: "union", Name: "The Union",                       Type: faction.FactionTypeResistance},
        },
        Tiers: tiers,
        Relations: []faction.Relation{
            {FactionA: "iar",   FactionB: "union", Influence: -0.8},
            {FactionA: "union", FactionB: "iar",   Influence: -0.8},
        },
        Store: newMemoryStore(), // swap for adapters/postgres in production
    })
    if err != nil {
        panic(err)
    }

    result, err := engine.ApplyDelta(ctx, faction.Delta{
        EntityID:  "smuggler-001",
        FactionID: "union",
        Amount:    20,
        Reason:    "Delivered guerilla weapons to Union contact",
        Source:    "smuggler_delivery_quest",
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("orders computed: %d  converged: %v\n",
        result.OrdersComputed, result.Converged)

    d, err := engine.GetDisposition(ctx, "smuggler-001", "union")
    if err != nil {
        panic(err)
    }

    fmt.Printf("union disposition: %d (%s)\n", d.Value, d.Tier.Name)
}

// memoryStore is a minimal in-memory Store for getting started.
// Replace with adapters/postgres for production use.
type memoryStore struct {
    mu           sync.Mutex
    dispositions map[string]faction.StoredDisposition
    history      []faction.DispositionRecord
}

func newMemoryStore() *memoryStore {
    return &memoryStore{dispositions: make(map[string]faction.StoredDisposition)}
}

func (s *memoryStore) key(e faction.EntityID, f faction.FactionID) string {
    return string(e) + "/" + string(f)
}

func (s *memoryStore) GetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID) (faction.StoredDisposition, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    return s.dispositions[s.key(e, f)], nil
}

func (s *memoryStore) SetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID, v int64) error {
    s.mu.Lock(); defer s.mu.Unlock()
    s.dispositions[s.key(e, f)] = faction.StoredDisposition{FactionID: f, Value: v, UpdatedAt: time.Now()}
    return nil
}

func (s *memoryStore) GetAllDispositions(_ context.Context, e faction.EntityID) ([]faction.StoredDisposition, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    prefix := string(e) + "/"
    var out []faction.StoredDisposition
    for k, v := range s.dispositions {
        if len(k) > len(prefix) && k[:len(prefix)] == prefix {
            out = append(out, v)
        }
    }
    return out, nil
}

func (s *memoryStore) GetDispositionHistory(_ context.Context, _ faction.EntityID, _ faction.FactionID, limit int) ([]faction.DispositionRecord, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    if limit > len(s.history) { limit = len(s.history) }
    return s.history[len(s.history)-limit:], nil
}

func (s *memoryStore) RecordDispositionChange(_ context.Context, r faction.DispositionRecord) error {
    s.mu.Lock(); defer s.mu.Unlock()
    s.history = append(s.history, r)
    return nil
}
```

## Tiers

Tiers define the named disposition threshold bands. The engine sorts tiers by `MinValue`, assigns rank automatically, and validates that tiers are contiguous with no gaps or overlaps. You never set or manage rank values manually.

### EvenTiers

Divides the disposition range evenly across any number of named tiers. Adding or removing a tier name recalculates all boundaries automatically.

`dispositionMin` and `dispositionMax` define the full extent of the disposition scale for your game. All player standings are clamped to this range.

```go
// Six equal tiers
tiers, err := faction.EvenTiers(
    -1000, // dispositionMin
    1000,  // dispositionMax
    "Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Celebrated",
)

// Adding a tier — just insert the name, no boundary recalculation needed
tiers, err := faction.EvenTiers(
    -1000, // dispositionMin
    1000,  // dispositionMax
    "Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Revered", "Celebrated",
)
```

Works with any number of factions and any number of tiers. There is no fixed tier count requirement — three tiers, eight tiers, or twelve tiers all work identically.

### WeightedTiers

Divides the range proportionally by weight. Useful when you want some tiers to be wider than others — a wider Neutral zone makes standing feel stable, narrower hostile zones make them hard to reach.

```go
tiers, err := faction.WeightedTiers(
    -1000, // dispositionMin
    1000,  // dispositionMax
    []faction.TierWeight{
        {Name: "Outlawed",   Weight: 1},
        {Name: "Wanted",     Weight: 1},
        {Name: "Suspected",  Weight: 1},
        {Name: "Neutral",    Weight: 3}, // three times wider than others
        {Name: "Trusted",    Weight: 2},
        {Name: "Celebrated", Weight: 1},
    },
)
```

### Manual tiers

For exact boundary control, construct `Tier` values directly. Tiers may be provided in any order — the engine sorts them. The engine validates contiguity and returns a descriptive error for any gap or overlap.

```go
Tiers: []faction.Tier{
    {Name: "Suspected", MinValue: -200, MaxValue: -1},
    {Name: "Neutral",   MinValue: 0,    MaxValue: 299},
    {Name: "Trusted",   MinValue: 300,  MaxValue: 699},
},
```

## The Taylor expansion in practice

The engine derives `DispositionRange` automatically from your tier configuration. For tiers spanning `-1000` to `+1000` the range is `2000`.

### Routine action — delta=100, range=2000, δ_n=0.05

| Order | Faction | Computation                          | Effect  |
|-------|---------|--------------------------------------|---------|
| 1     | union   | `100` (direct, always exact)         | +100    |
| 2     | iar     | `0.05² / 2! × -0.8 × 2000`          | -2      |
| 3     | —       | below threshold, expansion converged | —       |

A routine delivery quest produces a negligible indirect political effect. Hundreds of them over a character's lifetime accumulate into a meaningful signal.

### Major event — delta=800, range=2000, δ_n=0.4

| Order | Faction | Computation                          | Effect  |
|-------|---------|--------------------------------------|---------|
| 1     | union   | `800` (direct, always exact)         | +800    |
| 2     | iar     | `0.4² / 2! × -0.8 × 2000`           | -128    |
| 3     | union   | `0.4³ / 3! × 0.64 × 2000`           | +14     |

A city switching political alignment generates real ripples across three orders. The entire faction graph reacts.

## Action significance tiers

The mathematics distinguish three natural tiers of political significance:

| Delta range  | Behavior                                                     |
|--------------|--------------------------------------------------------------|
| < 200        | Only the direct first order effect matters                   |
| 200 – 600    | Second order effects register with nearby factions           |
| 600 – 1000   | Full ripple across the graph — the galaxy notices            |

These tiers emerge from the normalization. No configuration required.

## Decay

Disposition decay is evaluated lazily at read time — no background service required. When `GetDisposition` is called the engine computes how much time has elapsed since the value was last written and applies the configured rate before returning.

```go
engine, err := faction.New(faction.Config{
    // ...
    Decay: faction.DecayConfig{
        Enabled:     true,
        RatePerHour: 20,        // 20 disposition points per real-world hour
        Target:      "neutral", // decay toward neutral midpoint
                                // "zero" is the other option
    },
})
```

A character who earns high IAR standing and goes offline for 24 hours returns to find their standing has moved toward neutral — exactly as if the world continued without them. Logging out cannot be used to freeze disposition in a favorable state.

## Configuration

### TaylorConfig

| Field        | Default | Description                                                        |
|--------------|---------|--------------------------------------------------------------------|
| `MaxOrder`   | `3`     | Maximum Taylor expansion orders to compute                         |
| `Threshold`  | `0.5`   | Minimum damped effect magnitude below which expansion terminates   |

`DispositionRange` is derived automatically from the tier configuration and does not need to be set manually.

Higher `MaxOrder` values capture more distant indirect political effects. For most games `MaxOrder: 3` captures all meaningful ripple effects while remaining sub-microsecond per operation.

### Relation.Influence

| Value  | Meaning                                                         |
|--------|-----------------------------------------------------------------|
| `-1.0` | Perfect opposition — every gain with A is a full loss with B   |
| `-0.8` | Strong opposition — the IAR/Union default                       |
| `0.0`  | No relationship — factions are politically independent          |
| `+0.5` | Alliance — gains with A produce proportional gains with B      |
| `+1.0` | Perfect alliance — every gain with A is a full gain with B     |

## Interfaces

`cosfaction` requires one interface and optionally accepts two more. Bring your own implementations.

### Store (required)

```go
type Store interface {
    GetDisposition(ctx, entityID, factionID) (StoredDisposition, error)
    SetDisposition(ctx, entityID, factionID, value int64) error
    GetAllDispositions(ctx, entityID) ([]StoredDisposition, error)
    GetDispositionHistory(ctx, entityID, factionID, limit int) ([]DispositionRecord, error)
    RecordDispositionChange(ctx, record DispositionRecord) error
}
```

`StoredDisposition` carries the value and its `UpdatedAt` timestamp. The engine uses the timestamp to compute lazy decay on every read — the Store does not need to perform any decay logic itself.

### Cache (optional)

```go
type Cache interface {
    GetDisposition(ctx, entityID, factionID) (StoredDisposition, bool, error)
    SetDisposition(ctx, entityID, factionID, stored StoredDisposition) error
    Invalidate(ctx, entityID, factionID) error
    InvalidateAll(ctx, entityID) error
}
```

### Publisher (optional)

```go
type Publisher interface {
    Publish(event Event) error
}
```

Tier crossing events are the most useful for downstream game systems — enforcement probability recalculation, NPC behavior updates, and UI notifications should subscribe to `disposition.tier_crossed`.

## Events

Every disposition change — direct and propagated — emits an event:

```go
type Event struct {
    Type             EventType   // disposition.changed, disposition.tier_crossed, etc.
    EntityID         EntityID    // who changed
    FactionID        FactionID   // which faction changed
    PreviousValue    int64       // before
    NewValue         int64       // after
    PreviousTier     string      // named tier before
    NewTier          string      // named tier after
    TierChanged      bool        // did this cross a tier boundary
    PropagationOrder int         // 1=direct, 2+=indirect Taylor term
    Reason           string      // human readable reason
    Source           string      // originating game action
    OccurredAt       time.Time
}
```

## Adapters

Ready-made adapter implementations are available as separate sub-modules so your project only pulls in the dependencies it needs. Each adapter wraps your existing connection — cosfaction does not manage connections, credentials, or infrastructure.

| Adapter    | Module                                              | Implements  |
|------------|-----------------------------------------------------|-------------|
| PostgreSQL | `github.com/d1sco/cosfaction/adapters/postgres`     | `Store`     |
| Redis      | `github.com/d1sco/cosfaction/adapters/redis`        | `Cache`     |
| NATS       | `github.com/d1sco/cosfaction/adapters/nats`         | `Publisher` |

Install only what your backend already uses:

```bash
# Store — required, picks up where your existing Postgres pool is
go get github.com/d1sco/cosfaction/adapters/postgres@v0.1.1

# Cache — optional, sits in front of the store
go get github.com/d1sco/cosfaction/adapters/redis@v0.1.1

# Publisher — optional, routes events to downstream services
go get github.com/d1sco/cosfaction/adapters/nats@v0.1.1
```

### Wiring into an existing backend

The pattern is always the same: pass your existing connection into the adapter, pass the adapter into the engine. Nothing else changes in your infrastructure.

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    faction   "github.com/d1sco/cosfaction"
    pgadapter "github.com/d1sco/cosfaction/adapters/postgres"
    rdadapter "github.com/d1sco/cosfaction/adapters/redis"
    natsadapter "github.com/d1sco/cosfaction/adapters/nats"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/nats-io/nats.go"
    "github.com/redis/go-redis/v9"
)

func main() {
    ctx := context.Background()

    // ── PostgreSQL ────────────────────────────────────────────────
    // Pass your existing pool — cosfaction does not open connections.
    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }

    // Migrate is idempotent — safe to call on every startup.
    // Creates faction_dispositions and faction_disposition_history
    // tables if they do not already exist.
    if err := pgadapter.Migrate(ctx, pool); err != nil {
        log.Fatal(err)
    }

    store := pgadapter.New(pool)

    // ── Redis ─────────────────────────────────────────────────────
    // Optional. Sits in front of Postgres for hot-path disposition reads.
    rdb := redis.NewClient(&redis.Options{
        Addr: os.Getenv("REDIS_ADDR"), // e.g. "localhost:6379"
    })

    cache := rdadapter.New(rdb, rdadapter.Config{
        TTL:       24 * time.Hour,    // evict after 24 hours of inactivity
        KeyPrefix: "faction:",        // namespace within your Redis instance
    })

    // ── NATS ──────────────────────────────────────────────────────
    // Optional. Publishes disposition events to downstream services.
    // Events land at {SubjectPrefix}.disposition.{type}
    // e.g. game.faction.disposition.tier_crossed
    nc, err := nats.Connect(os.Getenv("NATS_URL"))
    if err != nil {
        log.Fatal(err)
    }

    publisher := natsadapter.New(nc, natsadapter.Config{
        SubjectPrefix: "game.faction", // scopes events to your game namespace
    })

    // ── Engine ────────────────────────────────────────────────────
    tiers, err := faction.EvenTiers(
        -10000, // dispositionMin
        10000,  // dispositionMax
        "Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Celebrated",
    )
    if err != nil {
        log.Fatal(err)
    }

    engine, err := faction.New(faction.Config{
        Factions: []faction.Faction{
            {ID: "iar",   Name: "Interstellar Authority Republic", Type: faction.FactionTypeGoverning},
            {ID: "union", Name: "The Union",                       Type: faction.FactionTypeResistance},
        },
        Tiers:     tiers,
        Relations: []faction.Relation{
            {FactionA: "iar",   FactionB: "union", Influence: -0.8},
            {FactionA: "union", FactionB: "iar",   Influence: -0.8},
        },
        Store:     store,     // required
        Cache:     cache,     // optional — remove if not using Redis
        Publisher: publisher, // optional — remove if not using NATS
        Decay: faction.DecayConfig{
            Enabled:     true,
            RatePerHour: 20,
            Target:      "neutral",
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    // Engine is ready. Use it anywhere in your game server.
    _ = engine
}
```

### Subscribing to events on your existing NATS consumer

```go
// Receive all faction events
nc.Subscribe("game.faction.>", func(msg *nats.Msg) {
    // msg.Data is a JSON-encoded faction.Event
})

// React only to tier crossings — the most actionable signal
nc.Subscribe("game.faction.disposition.tier.crossed", func(msg *nats.Msg) {
    var event faction.Event
    json.Unmarshal(msg.Data, &event)
    // recalculate enforcement probability, update NPC behaviour, notify player
})
```

## Performance

```
BenchmarkExpand_TwoFactions    2,581,891 ops/s    458 ns/op    392 B     9 allocs
BenchmarkExpand_FiveFactions   1,250,686 ops/s    973 ns/op   1144 B    18 allocs
```

Sub-microsecond for typical game faction graphs. Safe for hot paths including per-action and per-quest evaluation.

## Examples

- [`examples/basic`](examples/basic) — minimal single-faction setup, tier resolution, API walkthrough
- [`examples/decay`](examples/decay) — lazy decay evaluation over simulated time, toward-neutral and toward-zero targets
- [`examples/multi_faction`](examples/multi_faction) — three-faction graph with alliance and opposition relationships, full Taylor expansion output
- [`examples/events`](examples/events) — Publisher integration with realistic downstream subscribers: enforcement probability, player notifications, audit logging
- [`examples/clusters`](examples/clusters) — full IAR/Union political system from a sci-fi MMO, four scenarios from routine action to major world event

## License

MIT