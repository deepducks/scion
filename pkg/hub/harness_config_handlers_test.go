//go:build !no_sqlite

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
	"net/http"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/store"
)

func TestHarnessConfigList(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	hc := &store.HarnessConfig{
		ID:         "hc_test1",
		Slug:       "test-hc",
		Name:       "Test HC",
		Harness:    "claude",
		Scope:      "global",
		Visibility: store.VisibilityPublic,
		Status:     store.HarnessConfigStatusActive,
		Created:    time.Now(),
		Updated:    time.Now(),
	}
	if err := s.CreateHarnessConfig(ctx, hc); err != nil {
		t.Fatalf("failed to create harness config: %v", err)
	}

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/harness-configs", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ListHarnessConfigsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.HarnessConfigs) != 1 {
		t.Errorf("expected 1 harness config, got %d", len(resp.HarnessConfigs))
	}
}

func TestHarnessConfigCreate(t *testing.T) {
	srv, _ := testServer(t)

	body := map[string]interface{}{
		"slug":       "new-hc",
		"name":       "New HC",
		"harness":    "claude",
		"scope":      "global",
		"visibility": "private",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/harness-configs", body)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp CreateHarnessConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.HarnessConfig == nil {
		t.Fatalf("expected harness config in response, got nil")
	}

	if resp.HarnessConfig.Slug != "new-hc" {
		t.Errorf("expected slug 'new-hc', got %q", resp.HarnessConfig.Slug)
	}

	if resp.HarnessConfig.Visibility != store.VisibilityPrivate {
		t.Errorf("expected visibility 'private', got %q", resp.HarnessConfig.Visibility)
	}

	if resp.HarnessConfig.Status != store.HarnessConfigStatusActive {
		t.Errorf("expected status 'active' (no files), got %q", resp.HarnessConfig.Status)
	}
}

func TestHarnessConfigGet(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	hc := &store.HarnessConfig{
		ID:         "hc_get1",
		Slug:       "get-test",
		Name:       "Get Test",
		Harness:    "gemini",
		Scope:      "global",
		Visibility: store.VisibilityPublic,
		Status:     store.HarnessConfigStatusActive,
		Created:    time.Now(),
		Updated:    time.Now(),
	}
	if err := s.CreateHarnessConfig(ctx, hc); err != nil {
		t.Fatalf("failed to create harness config: %v", err)
	}

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/harness-configs/hc_get1", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result store.HarnessConfig
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Name != "Get Test" {
		t.Errorf("expected name 'Get Test', got %q", result.Name)
	}
	if result.Harness != "gemini" {
		t.Errorf("expected harness 'gemini', got %q", result.Harness)
	}
}

func TestHarnessConfigDelete(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	hc := &store.HarnessConfig{
		ID:         "hc_del1",
		Slug:       "del-test",
		Name:       "Del Test",
		Harness:    "claude",
		Scope:      "global",
		Visibility: store.VisibilityPublic,
		Status:     store.HarnessConfigStatusActive,
		Created:    time.Now(),
		Updated:    time.Now(),
	}
	if err := s.CreateHarnessConfig(ctx, hc); err != nil {
		t.Fatalf("failed to create harness config: %v", err)
	}

	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/harness-configs/hc_del1", nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted
	rec = doRequest(t, srv, http.MethodGet, "/api/v1/harness-configs/hc_del1", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 after delete, got %d", rec.Code)
	}
}

func TestHarnessConfigPatch(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	hc := &store.HarnessConfig{
		ID:         "hc_patch1",
		Slug:       "patch-test",
		Name:       "Patch Test",
		Harness:    "claude",
		Scope:      "global",
		Visibility: store.VisibilityPublic,
		Status:     store.HarnessConfigStatusActive,
		Created:    time.Now(),
		Updated:    time.Now(),
	}
	if err := s.CreateHarnessConfig(ctx, hc); err != nil {
		t.Fatalf("failed to create harness config: %v", err)
	}

	body := map[string]interface{}{
		"displayName": "Updated Display Name",
		"description": "Updated description",
	}

	rec := doRequest(t, srv, http.MethodPatch, "/api/v1/harness-configs/hc_patch1", body)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result store.HarnessConfig
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.DisplayName != "Updated Display Name" {
		t.Errorf("expected display name 'Updated Display Name', got %q", result.DisplayName)
	}
	if result.Description != "Updated description" {
		t.Errorf("expected description 'Updated description', got %q", result.Description)
	}
}
