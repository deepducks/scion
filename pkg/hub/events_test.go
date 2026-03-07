// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/store"
)

func TestSubjectMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		subject string
		want    bool
	}{
		{"exact match", "agent.123.status", "agent.123.status", true},
		{"no match different token", "agent.123.status", "agent.456.status", false},
		{"no match extra tokens", "agent.123", "agent.123.status", false},
		{"no match fewer tokens", "agent.123.status", "agent.123", false},
		{"star matches single token", "agent.*.status", "agent.123.status", true},
		{"star matches single token middle", "grove.*.agent.status", "grove.g1.agent.status", true},
		{"star does not match multiple tokens", "agent.*.status", "agent.123.456.status", false},
		{"gt matches remainder", "grove.>", "grove.g1.agent.status", true},
		{"gt matches single remaining", "grove.>", "grove.g1", true},
		{"gt does not match zero remaining", "grove.>", "grove", false},
		{"gt at start matches all", ">", "agent.123.status", true},
		{"empty pattern empty subject", "", "", true},
		{"combined star and literal", "agent.*.created", "agent.abc.created", true},
		{"combined star and literal no match", "agent.*.created", "agent.abc.deleted", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subjectMatchesPattern(tt.pattern, tt.subject)
			if got != tt.want {
				t.Errorf("subjectMatchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
			}
		})
	}
}

func TestChannelEventPublisher_PublishAgentStatus(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	// Subscribe to agent-specific and grove-scoped subjects
	agentCh, unsub1 := pub.Subscribe("agent.a1.status")
	defer unsub1()
	groveCh, unsub2 := pub.Subscribe("grove.g1.agent.status")
	defer unsub2()

	agent := &store.Agent{
		ID:      "a1",
		GroveID: "g1",
		Phase:   "running",
	}

	pub.PublishAgentStatus(context.Background(), agent)

	// Verify agent-specific event
	select {
	case evt := <-agentCh:
		if evt.Subject != "agent.a1.status" {
			t.Errorf("got subject %q, want %q", evt.Subject, "agent.a1.status")
		}
		var data AgentStatusEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.AgentID != "a1" || data.Phase != "running" || data.GroveID != "g1" {
			t.Errorf("unexpected event data: %+v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for agent event")
	}

	// Verify grove-scoped event
	select {
	case evt := <-groveCh:
		if evt.Subject != "grove.g1.agent.status" {
			t.Errorf("got subject %q, want %q", evt.Subject, "grove.g1.agent.status")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for grove event")
	}
}

func TestChannelEventPublisher_PublishAgentStatus_IncludesTurnCounts(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch, unsub := pub.Subscribe("agent.a1.status")
	defer unsub()

	agent := &store.Agent{
		ID:                "a1",
		GroveID:           "g1",
		Phase:             "running",
		Activity:          "thinking",
		CurrentTurns:      5,
		CurrentModelCalls: 12,
		StartedAt:         time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC),
	}

	pub.PublishAgentStatus(context.Background(), agent)

	select {
	case evt := <-ch:
		var data AgentStatusEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.Detail == nil {
			t.Fatal("expected detail to be set")
		}
		if data.Detail.CurrentTurns != 5 {
			t.Errorf("got currentTurns=%d, want 5", data.Detail.CurrentTurns)
		}
		if data.Detail.CurrentModelCalls != 12 {
			t.Errorf("got currentModelCalls=%d, want 12", data.Detail.CurrentModelCalls)
		}
		if data.Detail.StartedAt == "" {
			t.Error("expected startedAt to be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for agent event")
	}
}

func TestChannelEventPublisher_PublishAgentCreated(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	agentCh, unsub1 := pub.Subscribe("agent.a1.created")
	defer unsub1()
	groveCh, unsub2 := pub.Subscribe("grove.g1.agent.created")
	defer unsub2()

	agent := &store.Agent{
		ID:       "a1",
		GroveID:  "g1",
		Name:     "test-agent",
		Slug:     "test-agent",
		Template: "claude",
	}

	pub.PublishAgentCreated(context.Background(), agent)

	select {
	case evt := <-agentCh:
		var data AgentCreatedEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.AgentID != "a1" || data.Name != "test-agent" || data.Template != "claude" {
			t.Errorf("unexpected event data: %+v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for agent created event")
	}

	select {
	case evt := <-groveCh:
		if evt.Subject != "grove.g1.agent.created" {
			t.Errorf("got subject %q, want %q", evt.Subject, "grove.g1.agent.created")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for grove agent created event")
	}
}

func TestChannelEventPublisher_PublishAgentDeleted(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	agentCh, unsub1 := pub.Subscribe("agent.a1.deleted")
	defer unsub1()
	groveCh, unsub2 := pub.Subscribe("grove.g1.agent.deleted")
	defer unsub2()

	pub.PublishAgentDeleted(context.Background(), "a1", "g1")

	select {
	case evt := <-agentCh:
		var data AgentDeletedEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.AgentID != "a1" || data.GroveID != "g1" {
			t.Errorf("unexpected event data: %+v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for agent deleted event")
	}

	select {
	case evt := <-groveCh:
		if evt.Subject != "grove.g1.agent.deleted" {
			t.Errorf("got subject %q, want %q", evt.Subject, "grove.g1.agent.deleted")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for grove agent deleted event")
	}
}

func TestChannelEventPublisher_PublishGroveCreated(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch, unsub := pub.Subscribe("grove.g1.created")
	defer unsub()

	grove := &store.Grove{
		ID:   "g1",
		Name: "My Grove",
		Slug: "my-grove",
	}

	pub.PublishGroveCreated(context.Background(), grove)

	select {
	case evt := <-ch:
		if evt.Subject != "grove.g1.created" {
			t.Errorf("got subject %q, want %q", evt.Subject, "grove.g1.created")
		}
		var data GroveCreatedEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.GroveID != "g1" || data.Name != "My Grove" || data.Slug != "my-grove" {
			t.Errorf("unexpected event data: %+v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for grove created event")
	}
}

func TestChannelEventPublisher_PublishBrokerConnected(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch1, unsub1 := pub.Subscribe("grove.g1.broker.status")
	defer unsub1()
	ch2, unsub2 := pub.Subscribe("grove.g2.broker.status")
	defer unsub2()

	pub.PublishBrokerConnected(context.Background(), "b1", "broker-1", []string{"g1", "g2"})

	for _, tc := range []struct {
		ch      <-chan Event
		groveID string
	}{
		{ch1, "g1"},
		{ch2, "g2"},
	} {
		select {
		case evt := <-tc.ch:
			var data BrokerGroveEvent
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if data.BrokerID != "b1" || data.GroveID != tc.groveID || data.Status != "online" || data.BrokerName != "broker-1" {
				t.Errorf("unexpected event data for grove %s: %+v", tc.groveID, data)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for broker connected event for grove %s", tc.groveID)
		}
	}
}

func TestChannelEventPublisher_Backpressure(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch, unsub := pub.Subscribe("agent.>")
	defer unsub()

	agent := &store.Agent{
		ID:      "a1",
		GroveID: "g1",
		Phase:   "running",
	}

	// Fill the buffer (capacity 64) and then some
	for i := 0; i < 100; i++ {
		pub.PublishAgentStatus(context.Background(), agent)
	}

	// Should not block — drain what we can
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	// Buffer is 64, so we should get at most 64 events (only agent.a1.status matches)
	if count == 0 {
		t.Error("expected some events to be received")
	}
	if count > 64 {
		t.Errorf("received more events than buffer allows: %d", count)
	}
	// We sent 100 but buffer is 64, so some should have been dropped
	if count == 100 {
		t.Error("expected some events to be dropped due to backpressure")
	}
}

func TestChannelEventPublisher_SubscribeUnsubscribe(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch, unsub := pub.Subscribe("agent.a1.status")

	agent := &store.Agent{
		ID:      "a1",
		GroveID: "g1",
		Phase:   "running",
	}

	// Should receive before unsub
	pub.PublishAgentStatus(context.Background(), agent)
	select {
	case <-ch:
		// ok
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event before unsub")
	}

	// Unsubscribe
	unsub()

	// Should NOT receive after unsub
	pub.PublishAgentStatus(context.Background(), agent)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: no event received
	}
}

func TestChannelEventPublisher_Close(t *testing.T) {
	pub := NewChannelEventPublisher()

	ch, _ := pub.Subscribe("agent.>")

	pub.Close()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after Close()")
	}

	// Double close should not panic
	pub.Close()
}

func TestChannelEventPublisher_WildcardSubscription(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	// Subscribe to all grove events with >
	ch, unsub := pub.Subscribe("grove.>")
	defer unsub()

	grove := &store.Grove{
		ID:   "g1",
		Name: "Test",
		Slug: "test",
	}

	pub.PublishGroveCreated(context.Background(), grove)

	select {
	case evt := <-ch:
		if evt.Subject != "grove.g1.created" {
			t.Errorf("got subject %q, want %q", evt.Subject, "grove.g1.created")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for wildcard event")
	}
}

func TestChannelEventPublisher_PublishNotification(t *testing.T) {
	pub := NewChannelEventPublisher()
	defer pub.Close()

	ch, unsub := pub.Subscribe("notification.>")
	defer unsub()

	notif := &store.Notification{
		ID:        "n1",
		AgentID:   "a1",
		GroveID:   "g1",
		Status:    "COMPLETED",
		Message:   "test-agent has reached a state of COMPLETED",
		CreatedAt: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	pub.PublishNotification(context.Background(), notif)

	select {
	case evt := <-ch:
		if evt.Subject != "notification.created" {
			t.Errorf("got subject %q, want %q", evt.Subject, "notification.created")
		}
		var data NotificationCreatedEvent
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if data.ID != "n1" {
			t.Errorf("got ID %q, want %q", data.ID, "n1")
		}
		if data.AgentID != "a1" {
			t.Errorf("got AgentID %q, want %q", data.AgentID, "a1")
		}
		if data.GroveID != "g1" {
			t.Errorf("got GroveID %q, want %q", data.GroveID, "g1")
		}
		if data.Status != "COMPLETED" {
			t.Errorf("got Status %q, want %q", data.Status, "COMPLETED")
		}
		if data.Message != "test-agent has reached a state of COMPLETED" {
			t.Errorf("got Message %q, want expected message", data.Message)
		}
		if data.CreatedAt != "2026-03-01T12:00:00.000Z" {
			t.Errorf("got CreatedAt %q, want %q", data.CreatedAt, "2026-03-01T12:00:00.000Z")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notification event")
	}
}

func TestNoopEventPublisher(t *testing.T) {
	var pub noopEventPublisher
	ctx := context.Background()

	// None of these should panic
	pub.PublishAgentStatus(ctx, &store.Agent{})
	pub.PublishAgentCreated(ctx, &store.Agent{})
	pub.PublishAgentDeleted(ctx, "", "")
	pub.PublishGroveCreated(ctx, &store.Grove{})
	pub.PublishGroveUpdated(ctx, &store.Grove{})
	pub.PublishGroveDeleted(ctx, "")
	pub.PublishBrokerConnected(ctx, "", "", nil)
	pub.PublishBrokerDisconnected(ctx, "", nil)
	pub.PublishBrokerStatus(ctx, "", "")
	pub.PublishNotification(ctx, &store.Notification{})
	pub.Close()
}
