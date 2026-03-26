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
	"log/slog"
	"net/http"
	"strconv"

	"github.com/GoogleCloudPlatform/scion/pkg/store"
)

// handleMessages handles GET /api/v1/messages.
// Lists messages for the authenticated user.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowed(w)
		return
	}

	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	q := r.URL.Query()

	filter := store.MessageFilter{
		RecipientID: user.ID(),
	}
	if q.Get("unread") == "true" {
		filter.OnlyUnread = true
	}
	if groveID := q.Get("grove"); groveID != "" {
		filter.GroveID = groveID
	}
	if agentID := q.Get("agent"); agentID != "" {
		filter.AgentID = agentID
	}
	if msgType := q.Get("type"); msgType != "" {
		filter.Type = msgType
	}

	opts := store.ListOptions{}
	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if cursor := q.Get("cursor"); cursor != "" {
		opts.Cursor = cursor
	}

	result, err := s.store.ListMessages(r.Context(), filter, opts)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleMessageRoutes handles requests under /api/v1/messages/.
// Routes:
//   - GET  /api/v1/messages/{id}        — Get a single message
//   - POST /api/v1/messages/{id}/read   — Mark a message as read
//   - POST /api/v1/messages/read-all    — Mark all messages as read
func (s *Server) handleMessageRoutes(w http.ResponseWriter, r *http.Request) {
	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	id, action := extractAction(r, "/api/v1/messages")

	// POST /api/v1/messages/read-all
	if id == "read-all" && r.Method == http.MethodPost {
		if err := s.store.MarkAllMessagesRead(r.Context(), user.ID()); err != nil {
			writeErrorFromErr(w, err, "")
			return
		}
		slog.Info("All messages marked as read", "userID", user.ID())
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if id == "" {
		NotFound(w, "Message")
		return
	}

	// POST /api/v1/messages/{id}/read
	if action == "read" && r.Method == http.MethodPost {
		// Verify the message is addressed to this user before marking read.
		msg, err := s.store.GetMessage(r.Context(), id)
		if err != nil {
			writeErrorFromErr(w, err, "Message")
			return
		}
		if msg.RecipientID != user.ID() {
			Forbidden(w)
			return
		}
		if err := s.store.MarkMessageRead(r.Context(), id); err != nil {
			writeErrorFromErr(w, err, "Message")
			return
		}
		slog.Info("Message marked as read", "messageID", id, "userID", user.ID())
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// GET /api/v1/messages/{id}
	if action == "" && r.Method == http.MethodGet {
		msg, err := s.store.GetMessage(r.Context(), id)
		if err != nil {
			writeErrorFromErr(w, err, "Message")
			return
		}
		// Only allow access to messages addressed to this user.
		if msg.RecipientID != user.ID() {
			Forbidden(w)
			return
		}
		writeJSON(w, http.StatusOK, msg)
		return
	}

	MethodNotAllowed(w)
}

// handleAgentMessages handles GET /api/v1/agents/{id}/messages.
// Returns messages involving a specific agent, scoped to the authenticated user.
func (s *Server) handleAgentMessages(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		MethodNotAllowed(w)
		return
	}

	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	q := r.URL.Query()
	opts := store.ListOptions{}
	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if cursor := q.Get("cursor"); cursor != "" {
		opts.Cursor = cursor
	}

	filter := store.MessageFilter{
		AgentID:     agentID,
		RecipientID: user.ID(),
	}

	result, err := s.store.ListMessages(r.Context(), filter, opts)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
