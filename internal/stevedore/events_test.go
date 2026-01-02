package stevedore

import (
	"testing"
	"time"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	eb := NewEventBus(10)

	// Subscribe
	ch := eb.Subscribe()
	defer eb.Unsubscribe(ch)

	// Publish event
	event := Event{
		Type:       EventParamsChanged,
		Deployment: "test-app",
		Details:    map[string]string{"key": "STEVEDORE_INGRESS_WEB_ENABLED"},
	}
	eb.Publish(event)

	// Receive event
	select {
	case received := <-ch:
		if received.Type != EventParamsChanged {
			t.Errorf("Type = %q, want %q", received.Type, EventParamsChanged)
		}
		if received.Deployment != "test-app" {
			t.Errorf("Deployment = %q, want %q", received.Deployment, "test-app")
		}
		if received.Timestamp.IsZero() {
			t.Error("Timestamp should be set automatically")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := NewEventBus(10)

	ch1 := eb.Subscribe()
	ch2 := eb.Subscribe()
	defer eb.Unsubscribe(ch1)
	defer eb.Unsubscribe(ch2)

	if eb.SubscriberCount() != 2 {
		t.Errorf("SubscriberCount = %d, want 2", eb.SubscriberCount())
	}

	// Publish event
	eb.Publish(Event{Type: EventDeploymentUpdated, Deployment: "app"})

	// Both should receive
	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Deployment != "app" {
				t.Errorf("Subscriber %d: Deployment = %q, want %q", i, received.Deployment, "app")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Subscriber %d: Timeout waiting for event", i)
		}
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus(10)

	ch := eb.Subscribe()
	if eb.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount = %d, want 1", eb.SubscriberCount())
	}

	eb.Unsubscribe(ch)
	if eb.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount after unsubscribe = %d, want 0", eb.SubscriberCount())
	}
}

func TestEventBus_EventsSince(t *testing.T) {
	eb := NewEventBus(10)

	// Publish some events
	t1 := time.Now()
	eb.Publish(Event{Type: EventDeploymentCreated, Deployment: "app1"})
	time.Sleep(10 * time.Millisecond)

	t2 := time.Now()
	eb.Publish(Event{Type: EventDeploymentUpdated, Deployment: "app2"})
	time.Sleep(10 * time.Millisecond)

	eb.Publish(Event{Type: EventParamsChanged, Deployment: "app3"})

	// Get events since t1 (should get all 3)
	events := eb.EventsSince(t1.Add(-time.Millisecond))
	if len(events) != 3 {
		t.Errorf("EventsSince(t1) returned %d events, want 3", len(events))
	}

	// Get events since t2 (should get 2)
	events = eb.EventsSince(t2)
	if len(events) != 2 {
		t.Errorf("EventsSince(t2) returned %d events, want 2", len(events))
	}

	// Get events since now (should get 0)
	events = eb.EventsSince(time.Now())
	if len(events) != 0 {
		t.Errorf("EventsSince(now) returned %d events, want 0", len(events))
	}
}

func TestEventBus_HistoryLimit(t *testing.T) {
	eb := NewEventBus(3) // Small history

	// Publish more events than history size
	for i := 0; i < 5; i++ {
		eb.Publish(Event{Type: EventDeploymentUpdated, Deployment: "app"})
	}

	// Should only have 3 events in history
	events := eb.EventsSince(time.Time{})
	if len(events) != 3 {
		t.Errorf("History size = %d, want 3", len(events))
	}
}

func TestEventBus_LastEventTime(t *testing.T) {
	eb := NewEventBus(10)

	// No events yet
	if !eb.LastEventTime().IsZero() {
		t.Error("LastEventTime should be zero when no events")
	}

	// Publish event
	beforePublish := time.Now()
	eb.Publish(Event{Type: EventDeploymentUpdated})
	afterPublish := time.Now()

	lastTime := eb.LastEventTime()
	if lastTime.Before(beforePublish) || lastTime.After(afterPublish) {
		t.Errorf("LastEventTime = %v, want between %v and %v", lastTime, beforePublish, afterPublish)
	}
}

func TestEventTypes(t *testing.T) {
	// Verify event type constants
	tests := []struct {
		name     string
		constant EventType
		want     string
	}{
		{"EventDeploymentCreated", EventDeploymentCreated, "deployment.created"},
		{"EventDeploymentUpdated", EventDeploymentUpdated, "deployment.updated"},
		{"EventDeploymentRemoved", EventDeploymentRemoved, "deployment.removed"},
		{"EventDeploymentStatusChanged", EventDeploymentStatusChanged, "deployment.status_changed"},
		{"EventParamsChanged", EventParamsChanged, "params.changed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.constant) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestEventBus_NonBlockingPublish(t *testing.T) {
	eb := NewEventBus(10)

	// Subscribe but don't read from channel
	ch := eb.Subscribe()
	defer eb.Unsubscribe(ch)

	// Publish more events than channel buffer
	for i := 0; i < 20; i++ {
		eb.Publish(Event{Type: EventDeploymentUpdated})
	}

	// Should not block - if this test completes, it passed
	// The subscriber channel should have 10 events (buffer size)
}
