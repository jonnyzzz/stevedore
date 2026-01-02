package stevedore

import (
	"sync"
	"time"
)

// EventType represents the type of change event.
type EventType string

const (
	// EventDeploymentCreated is emitted when a new deployment is added.
	EventDeploymentCreated EventType = "deployment.created"
	// EventDeploymentUpdated is emitted when a deployment is synced or deployed.
	EventDeploymentUpdated EventType = "deployment.updated"
	// EventDeploymentRemoved is emitted when a deployment is deleted.
	EventDeploymentRemoved EventType = "deployment.removed"
	// EventDeploymentStatusChanged is emitted when container health/state changes.
	EventDeploymentStatusChanged EventType = "deployment.status_changed"
	// EventParamsChanged is emitted when parameters are set or deleted.
	EventParamsChanged EventType = "params.changed"
)

// Event represents a change event in the system.
type Event struct {
	Type       EventType         `json:"type"`
	Deployment string            `json:"deployment,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
	Details    map[string]string `json:"details,omitempty"`
}

// EventBus provides pub/sub for change events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers []chan Event
	history     []Event
	historySize int
}

// NewEventBus creates a new event bus with the specified history size.
func NewEventBus(historySize int) *EventBus {
	if historySize <= 0 {
		historySize = 100
	}
	return &EventBus{
		historySize: historySize,
	}
}

// Publish sends an event to all subscribers.
func (eb *EventBus) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Add to history
	eb.history = append(eb.history, event)
	if len(eb.history) > eb.historySize {
		eb.history = eb.history[1:]
	}

	// Notify all subscribers
	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip this subscriber
		}
	}
}

// Subscribe returns a channel that receives events.
// The caller must call Unsubscribe when done.
func (eb *EventBus) Subscribe() chan Event {
	ch := make(chan Event, 10)

	eb.mu.Lock()
	eb.subscribers = append(eb.subscribers, ch)
	eb.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription channel.
func (eb *EventBus) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for i, sub := range eb.subscribers {
		if sub == ch {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// EventsSince returns all events after the given timestamp.
func (eb *EventBus) EventsSince(since time.Time) []Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var result []Event
	for _, event := range eb.history {
		if event.Timestamp.After(since) {
			result = append(result, event)
		}
	}
	return result
}

// LastEventTime returns the timestamp of the most recent event.
func (eb *EventBus) LastEventTime() time.Time {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if len(eb.history) == 0 {
		return time.Time{}
	}
	return eb.history[len(eb.history)-1].Timestamp
}

// SubscriberCount returns the number of active subscribers.
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}
