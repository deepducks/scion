package chatapp

import (
	"context"
	"log/slog"
	"testing"

	"github.com/GoogleCloudPlatform/scion/pkg/messages"
)

// TestHandleBrokerMessage_UserMessageRouting verifies that user-targeted
// messages with the full scion broker topic prefix are correctly routed
// to handleUserMessage.
func TestHandleBrokerMessage_UserMessageRouting(t *testing.T) {
	log := slog.Default()
	relay := NewNotificationRelay(nil, nil, log)

	// Message with empty RecipientID triggers early return in handleUserMessage
	// without touching the store, so we can test topic routing safely.
	msg := &messages.StructuredMessage{
		Sender: "agent:test-agent",
		Msg:    "hello from agent",
	}

	// Full scion-prefixed topic should route to handleUserMessage.
	err := relay.HandleBrokerMessage(context.Background(),
		"scion.grove.grove-123.user.user-456.messages", msg)
	if err != nil {
		t.Errorf("expected nil error for user message topic, got: %v", err)
	}
}

// TestHandleBrokerMessage_IgnoredTopics verifies that unrecognized or
// malformed topics are silently ignored.
func TestHandleBrokerMessage_IgnoredTopics(t *testing.T) {
	log := slog.Default()
	relay := NewNotificationRelay(nil, nil, log)
	msg := &messages.StructuredMessage{Msg: "test"}

	topics := []string{
		"x",
		"scion.global.broadcast",
		"user.user-456.message", // old unprefixed format
	}

	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			err := relay.HandleBrokerMessage(context.Background(), topic, msg)
			if err != nil {
				t.Errorf("expected nil error for ignored topic %q, got: %v", topic, err)
			}
		})
	}
}
