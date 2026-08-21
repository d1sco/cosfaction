// Package nats provides a NATS implementation of the faction.Publisher
// interface for use with the cosfaction disposition engine.
//
// The caller is responsible for establishing and managing the nats.Conn.
// This adapter performs no connection management of its own.
//
// Usage:
//
//	nc, err := nats.Connect(os.Getenv("NATS_URL"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	publisher := natsadapter.New(nc, natsadapter.Config{
//	    SubjectPrefix: "mygame.faction",
//	})
//
//	engine, err := faction.New(faction.Config{
//	    Store:     store,
//	    Cache:     cache,
//	    Publisher: publisher,
//	    // ...
//	})
//
// Subject routing:
//
// Each event type is published to a distinct NATS subject derived from the
// configured prefix and the event type. This allows downstream services to
// subscribe selectively using NATS subject patterns.
//
//	faction.events.disposition.changed
//	faction.events.disposition.tier_crossed
//	faction.events.disposition.decayed
//	faction.events.disposition.propagation_applied
//
// To receive all faction events:
//
//	nc.Subscribe("faction.events.>", handler)
//
// To receive only tier crossing events:
//
//	nc.Subscribe("faction.events.disposition.tier_crossed", handler)
//
// Events are serialized as JSON. The full faction.Event struct is published
// so downstream services have complete context without additional lookups.
package nats

import (
	"encoding/json"
	"fmt"
	"strings"

	faction "github.com/d1sco/cosfaction"
	"github.com/nats-io/nats.go"
)

const (
	// defaultSubjectPrefix is the NATS subject namespace for faction events.
	defaultSubjectPrefix = "faction.events"
)

// Config controls the behaviour of the NATS publisher adapter.
type Config struct {
	// SubjectPrefix is the root NATS subject under which all faction events
	// are published. Defaults to "faction.events".
	//
	// Example subject with default prefix and disposition.tier_crossed type:
	//   faction.events.disposition.tier_crossed
	//
	// Override this when multiple games or services share a NATS cluster
	// to prevent subject collisions:
	//   mygame.faction.events.disposition.tier_crossed
	SubjectPrefix string
}

// Publisher implements faction.Publisher against a NATS connection.
// All methods are safe for concurrent use — nats.Conn.Publish is goroutine safe.
type Publisher struct {
	nc     *nats.Conn
	prefix string
}

// New constructs a Publisher from an existing nats.Conn.
func New(nc *nats.Conn, cfg Config) *Publisher {
	prefix := cfg.SubjectPrefix
	if prefix == "" {
		prefix = defaultSubjectPrefix
	}

	return &Publisher{
		nc:     nc,
		prefix: prefix,
	}
}

// Publish serializes a faction.Event to JSON and publishes it to the
// NATS subject derived from the event type.
//
// The subject is built as: {prefix}.{event_type_with_dots}
//
// Example: "faction.events.disposition.tier_crossed"
//
// Publish is synchronous — it returns only after the message has been
// written to the NATS client's send buffer. For JetStream persistence
// use PublishAsync or replace this adapter with a JetStream publisher.
func (p *Publisher) Publish(event faction.Event) error {
	subject := p.subjectFor(event.Type)

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf(
			"cosfaction nats: marshal event %s: %w",
			event.Type, err,
		)
	}

	if err := p.nc.Publish(subject, payload); err != nil {
		return fmt.Errorf(
			"cosfaction nats: publish to %s: %w",
			subject, err,
		)
	}

	return nil
}

// subjectFor builds the NATS subject for a given event type.
// faction.EventType uses dot-separated strings which map directly
// to NATS subject hierarchy.
//
// "disposition.tier_crossed" → "faction.events.disposition.tier_crossed"
func (p *Publisher) subjectFor(eventType faction.EventType) string {
	// EventType values use dots as separators (disposition.changed).
	// Replace underscores with dots to produce clean NATS hierarchy.
	normalized := strings.ReplaceAll(string(eventType), "_", ".")
	return p.prefix + "." + normalized
}

// Subject returns the NATS subject that events of the given type will be
// published to. Useful for setting up subscriptions in downstream services.
func (p *Publisher) Subject(eventType faction.EventType) string {
	return p.subjectFor(eventType)
}

// WildcardSubject returns a NATS wildcard subject that matches all events
// published by this adapter. Useful for debugging and monitoring.
//
// Example: "faction.events.>"
func (p *Publisher) WildcardSubject() string {
	return p.prefix + ".>"
}
