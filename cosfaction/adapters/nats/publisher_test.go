package nats_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	faction "github.com/cosfaction/cosfaction"
	natsadapter "github.com/cosfaction/cosfaction/adapters/nats"
	"github.com/nats-io/nats.go"
)

// requireNATS returns a NATS connection for integration testing.
// Tests are skipped if NATS_URL is not set or if -short is passed.
//
// To run integration tests:
//
//	NATS_URL=nats://localhost:4222 go test ./...
//
// With Docker:
//
//	docker run --rm -p 4222:4222 nats:2.10
//	NATS_URL=nats://localhost:4222 go test ./...
func requireNATS(t *testing.T) *nats.Conn {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	url := os.Getenv("NATS_URL")
	if url == "" {
		t.Skip("skipping integration test: NATS_URL not set")
	}

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}

	t.Cleanup(func() {
		nc.Drain()
		nc.Close()
	})

	return nc
}

func testEvent(eventType faction.EventType) faction.Event {
	return faction.Event{
		Type:             eventType,
		EntityID:         "smuggler-001",
		FactionID:        "union",
		PreviousValue:    0,
		NewValue:         100,
		PreviousTier:     "Neutral",
		NewTier:          "Neutral",
		TierChanged:      false,
		PropagationOrder: 1,
		Reason:           "test event",
		Source:           "test",
		OccurredAt:       time.Now(),
	}
}

func TestPublisher_Publish_DispositionChanged(t *testing.T) {
	nc := requireNATS(t)
	pub := natsadapter.New(nc, natsadapter.Config{})
	ctx := context.Background()
	_ = ctx

	received := make(chan faction.Event, 1)

	subject := pub.Subject(faction.EventDispositionChanged)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var event faction.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			t.Errorf("failed to unmarshal event: %v", err)
			return
		}
		received <- event
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer sub.Unsubscribe()

	event := testEvent(faction.EventDispositionChanged)
	if err := pub.Publish(event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.EntityID != "smuggler-001" {
			t.Errorf("expected EntityID smuggler-001, got %s", got.EntityID)
		}
		if got.Type != faction.EventDispositionChanged {
			t.Errorf("expected type %s, got %s", faction.EventDispositionChanged, got.Type)
		}
		if got.NewValue != 100 {
			t.Errorf("expected NewValue 100, got %d", got.NewValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublisher_Publish_TierCrossed(t *testing.T) {
	nc := requireNATS(t)
	pub := natsadapter.New(nc, natsadapter.Config{})

	received := make(chan faction.Event, 1)

	subject := pub.Subject(faction.EventTierCrossed)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var event faction.Event
		json.Unmarshal(msg.Data, &event)
		received <- event
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer sub.Unsubscribe()

	event := testEvent(faction.EventTierCrossed)
	event.TierChanged = true
	event.NewTier = "Trusted"

	if err := pub.Publish(event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.Type != faction.EventTierCrossed {
			t.Errorf("expected tier_crossed, got %s", got.Type)
		}
		if got.NewTier != "Trusted" {
			t.Errorf("expected NewTier Trusted, got %s", got.NewTier)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tier crossed event")
	}
}

func TestPublisher_WildcardSubject_ReceivesAll(t *testing.T) {
	nc := requireNATS(t)
	pub := natsadapter.New(nc, natsadapter.Config{})

	receivedCount := 0
	done := make(chan struct{})

	sub, err := nc.Subscribe(pub.WildcardSubject(), func(msg *nats.Msg) {
		receivedCount++
		if receivedCount == 2 {
			close(done)
		}
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer sub.Unsubscribe()

	pub.Publish(testEvent(faction.EventDispositionChanged))
	pub.Publish(testEvent(faction.EventTierCrossed))

	select {
	case <-done:
		if receivedCount != 2 {
			t.Errorf("expected 2 events via wildcard, got %d", receivedCount)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out: only received %d of 2 events", receivedCount)
	}
}

func TestPublisher_SubjectRouting(t *testing.T) {
	nc := requireNATS(t)

	tests := []struct {
		cfg            natsadapter.Config
		eventType      faction.EventType
		expectedSubject string
	}{
		{
			cfg:            natsadapter.Config{},
			eventType:      faction.EventDispositionChanged,
			expectedSubject: "faction.events.disposition.changed",
		},
		{
			cfg:            natsadapter.Config{},
			eventType:      faction.EventTierCrossed,
			expectedSubject: "faction.events.disposition.tier.crossed",
		},
		{
			cfg:            natsadapter.Config{SubjectPrefix: "mygame.faction"},
			eventType:      faction.EventDispositionChanged,
			expectedSubject: "mygame.faction.disposition.changed",
		},
	}

	for _, tt := range tests {
		pub := natsadapter.New(nc, tt.cfg)
		got := pub.Subject(tt.eventType)
		if got != tt.expectedSubject {
			t.Errorf("Subject(%s) with prefix %q = %q, want %q",
				tt.eventType, tt.cfg.SubjectPrefix, got, tt.expectedSubject)
		}
	}
}

func TestPublisher_WildcardSubject_Format(t *testing.T) {
	nc := requireNATS(t)

	tests := []struct {
		prefix   string
		expected string
	}{
		{"", "faction.events.>"},
		{"mygame.faction", "mygame.faction.>"},
	}

	for _, tt := range tests {
		pub := natsadapter.New(nc, natsadapter.Config{SubjectPrefix: tt.prefix})
		got := pub.WildcardSubject()
		if got != tt.expected {
			t.Errorf("WildcardSubject() with prefix %q = %q, want %q",
				tt.prefix, got, tt.expected)
		}
	}
}

func TestPublisher_JSONSerialization(t *testing.T) {
	nc := requireNATS(t)
	pub := natsadapter.New(nc, natsadapter.Config{})

	received := make(chan []byte, 1)

	sub, err := nc.Subscribe(pub.WildcardSubject(), func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer sub.Unsubscribe()

	now := time.Now().Truncate(time.Second)
	event := faction.Event{
		Type:             faction.EventTierCrossed,
		EntityID:         "politician-001",
		FactionID:        "iar",
		PreviousValue:    -150,
		NewValue:         50,
		PreviousTier:     "Suspected",
		NewTier:          "Neutral",
		TierChanged:      true,
		PropagationOrder: 1,
		Reason:           "bounty completed",
		Source:           "bounty_contract",
		OccurredAt:       now,
	}

	if err := pub.Publish(event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case raw := <-received:
		var got faction.Event
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("failed to unmarshal received event: %v", err)
		}

		if got.EntityID != "politician-001" {
			t.Errorf("EntityID: got %s, want politician-001", got.EntityID)
		}
		if got.PreviousValue != -150 {
			t.Errorf("PreviousValue: got %d, want -150", got.PreviousValue)
		}
		if got.NewTier != "Neutral" {
			t.Errorf("NewTier: got %s, want Neutral", got.NewTier)
		}
		if !got.TierChanged {
			t.Error("TierChanged: got false, want true")
		}
		if got.Reason != "bounty completed" {
			t.Errorf("Reason: got %s, want 'bounty completed'", got.Reason)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}
