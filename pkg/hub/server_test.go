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

//go:build !no_sqlite

package hub

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/store"
	"github.com/GoogleCloudPlatform/scion/pkg/store/sqlite"
)

func TestServer_PersistentSigningKeys(t *testing.T) {
	// Create an in-memory SQLite store
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	cfg := DefaultServerConfig()

	// Create first server
	srv1 := New(cfg, s)
	t.Cleanup(func() { srv1.Shutdown(context.Background()) })
	if srv1.agentTokenService == nil {
		t.Fatal("agentTokenService not initialized in srv1")
	}
	if srv1.userTokenService == nil {
		t.Fatal("userTokenService not initialized in srv1")
	}

	key1 := srv1.agentTokenService.config.SigningKey
	userKey1 := srv1.userTokenService.config.SigningKey

	// Create second server with the same store
	srv2 := New(cfg, s)
	t.Cleanup(func() { srv2.Shutdown(context.Background()) })
	if srv2.agentTokenService == nil {
		t.Fatal("agentTokenService not initialized in srv2")
	}
	if srv2.userTokenService == nil {
		t.Fatal("userTokenService not initialized in srv2")
	}

	key2 := srv2.agentTokenService.config.SigningKey
	userKey2 := srv2.userTokenService.config.SigningKey

	// Check if keys match
	if string(key1) != string(key2) {
		t.Errorf("agent signing keys do not match: %x != %x", key1, key2)
	}
	if string(userKey1) != string(userKey2) {
		t.Errorf("user signing keys do not match: %x != %x", userKey1, userKey2)
	}
}

func TestServer_PersistentSigningKeys_WithHubID(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	cfg := DefaultServerConfig()
	cfg.HubID = "test-hub-123"

	srv1 := New(cfg, s)
	t.Cleanup(func() { srv1.Shutdown(context.Background()) })
	if srv1.agentTokenService == nil {
		t.Fatal("agentTokenService not initialized")
	}

	key1 := srv1.agentTokenService.config.SigningKey
	userKey1 := srv1.userTokenService.config.SigningKey

	// Second server with same hubID should get the same keys
	srv2 := New(cfg, s)
	t.Cleanup(func() { srv2.Shutdown(context.Background()) })

	if string(key1) != string(srv2.agentTokenService.config.SigningKey) {
		t.Error("agent signing keys should match with same hubID")
	}
	if string(userKey1) != string(srv2.userTokenService.config.SigningKey) {
		t.Error("user signing keys should match with same hubID")
	}

	// Signing keys should be visible when listing hub-scoped secrets
	ctx := context.Background()
	listed, err := s.ListSecrets(ctx, store.SecretFilter{Scope: store.ScopeHub, ScopeID: "test-hub-123"})
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}
	foundKeys := map[string]bool{}
	for _, sec := range listed {
		foundKeys[sec.Key] = true
	}
	if !foundKeys[SecretKeyAgentSigningKey] {
		t.Error("agent_signing_key should appear in hub secret list")
	}
	if !foundKeys[SecretKeyUserSigningKey] {
		t.Error("user_signing_key should appear in hub secret list")
	}
}

func TestServer_SigningKeyMigration_LegacyHubScopeID(t *testing.T) {
	// Simulate the pre-hubID-namespacing scenario where keys were stored
	// with ScopeID="hub". A new server with a real hubID should find them
	// via the migration fallback.
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	ctx := context.Background()

	// Directly insert legacy keys with ScopeID="hub" (simulating pre-refactor storage)
	legacyAgentKey := make([]byte, 32)
	legacyUserKey := make([]byte, 32)
	copy(legacyAgentKey, []byte("test-agent-key-1234567890123456"))
	copy(legacyUserKey, []byte("test-user-key-12345678901234567"))
	agentEncoded := base64.StdEncoding.EncodeToString(legacyAgentKey)
	userEncoded := base64.StdEncoding.EncodeToString(legacyUserKey)

	if err := s.CreateSecret(ctx, &store.Secret{
		ID: "hub-agent_signing_key", Key: SecretKeyAgentSigningKey,
		EncryptedValue: agentEncoded, Scope: store.ScopeHub, ScopeID: "hub",
		Description: "Hub signing key for agent_signing_key",
	}); err != nil {
		t.Fatalf("failed to create legacy agent key: %v", err)
	}
	if err := s.CreateSecret(ctx, &store.Secret{
		ID: "hub-user_signing_key", Key: SecretKeyUserSigningKey,
		EncryptedValue: userEncoded, Scope: store.ScopeHub, ScopeID: "hub",
		Description: "Hub signing key for user_signing_key",
	}); err != nil {
		t.Fatalf("failed to create legacy user key: %v", err)
	}

	// Now create a server with an actual hubID — it should migrate from "hub"
	cfg := DefaultServerConfig()
	cfg.HubID = "my-new-hub-id"
	srv := New(cfg, s)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	if string(legacyAgentKey) != string(srv.agentTokenService.config.SigningKey) {
		t.Error("agent signing key should be migrated from legacy 'hub' scope")
	}
	if string(legacyUserKey) != string(srv.userTokenService.config.SigningKey) {
		t.Error("user signing key should be migrated from legacy 'hub' scope")
	}

	// Verify the migrated keys are findable via ListSecrets with the new hub ID.
	// This is what the admin UI and CLI use to display hub-scoped secrets.
	listed, err := s.ListSecrets(ctx, store.SecretFilter{Scope: store.ScopeHub, ScopeID: "my-new-hub-id"})
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}
	foundKeys := map[string]bool{}
	for _, sec := range listed {
		foundKeys[sec.Key] = true
	}
	if !foundKeys[SecretKeyAgentSigningKey] {
		t.Error("agent_signing_key should be listed under new hub ID after migration")
	}
	if !foundKeys[SecretKeyUserSigningKey] {
		t.Error("user_signing_key should be listed under new hub ID after migration")
	}

	// Verify the old legacy records are cleaned up
	oldSecrets, err := s.ListSecrets(ctx, store.SecretFilter{Scope: store.ScopeHub, ScopeID: "hub"})
	if err != nil {
		t.Fatalf("ListSecrets (legacy) failed: %v", err)
	}
	for _, sec := range oldSecrets {
		if sec.Key == SecretKeyAgentSigningKey || sec.Key == SecretKeyUserSigningKey {
			t.Errorf("legacy record for %s should have been deleted during migration", sec.Key)
		}
	}
}

func TestServer_GenerateAgentToken_DevAuthAutoGrantsScopes(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	cfg := DefaultServerConfig()
	cfg.DevAuthToken = "test-dev-token"
	cfg.AgentTokenConfig = AgentTokenConfig{
		SigningKey:    make([]byte, 32),
		TokenDuration: time.Hour,
	}

	srv := New(cfg, s)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	// Generate token without any additional scopes
	token, err := srv.GenerateAgentToken("agent-1", "grove-1")
	if err != nil {
		t.Fatalf("GenerateAgentToken failed: %v", err)
	}

	// Validate the token and check scopes
	claims, err := srv.agentTokenService.ValidateAgentToken(token)
	if err != nil {
		t.Fatalf("ValidateAgentToken failed: %v", err)
	}

	if !claims.HasScope(ScopeAgentStatusUpdate) {
		t.Error("expected ScopeAgentStatusUpdate to be present")
	}
	if !claims.HasScope(ScopeAgentCreate) {
		t.Error("expected ScopeAgentCreate to be auto-granted in dev-auth mode")
	}
	if !claims.HasScope(ScopeAgentLifecycle) {
		t.Error("expected ScopeAgentLifecycle to be auto-granted in dev-auth mode")
	}
	if !claims.HasScope(ScopeAgentNotify) {
		t.Error("expected ScopeAgentNotify to be auto-granted in dev-auth mode")
	}
}

func TestServer_GenerateAgentToken_DevAuthDeduplicatesScopes(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	cfg := DefaultServerConfig()
	cfg.DevAuthToken = "test-dev-token"
	cfg.AgentTokenConfig = AgentTokenConfig{
		SigningKey:    make([]byte, 32),
		TokenDuration: time.Hour,
	}

	srv := New(cfg, s)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	// Generate token with explicit scopes that overlap with auto-granted ones
	token, err := srv.GenerateAgentToken("agent-1", "grove-1",
		ScopeAgentCreate, ScopeAgentLifecycle, ScopeGroveSecretRead)
	if err != nil {
		t.Fatalf("GenerateAgentToken failed: %v", err)
	}

	claims, err := srv.agentTokenService.ValidateAgentToken(token)
	if err != nil {
		t.Fatalf("ValidateAgentToken failed: %v", err)
	}

	// Count occurrences of each scope to verify deduplication
	scopeCounts := make(map[AgentTokenScope]int)
	for _, sc := range claims.Scopes {
		scopeCounts[sc]++
	}

	if scopeCounts[ScopeAgentCreate] != 1 {
		t.Errorf("expected ScopeAgentCreate once, got %d", scopeCounts[ScopeAgentCreate])
	}
	if scopeCounts[ScopeAgentLifecycle] != 1 {
		t.Errorf("expected ScopeAgentLifecycle once, got %d", scopeCounts[ScopeAgentLifecycle])
	}
	if !claims.HasScope(ScopeGroveSecretRead) {
		t.Error("expected ScopeGroveSecretRead to be present from explicit scopes")
	}
}

func TestServer_GenerateAgentToken_NoDevAuthDoesNotAutoGrant(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	cfg := DefaultServerConfig()
	// DevAuthToken is empty - not dev-auth mode
	cfg.AgentTokenConfig = AgentTokenConfig{
		SigningKey:    make([]byte, 32),
		TokenDuration: time.Hour,
	}

	srv := New(cfg, s)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	token, err := srv.GenerateAgentToken("agent-1", "grove-1")
	if err != nil {
		t.Fatalf("GenerateAgentToken failed: %v", err)
	}

	claims, err := srv.agentTokenService.ValidateAgentToken(token)
	if err != nil {
		t.Fatalf("ValidateAgentToken failed: %v", err)
	}

	if !claims.HasScope(ScopeAgentStatusUpdate) {
		t.Error("expected ScopeAgentStatusUpdate to be present")
	}
	if claims.HasScope(ScopeAgentCreate) {
		t.Error("expected ScopeAgentCreate NOT to be auto-granted without dev-auth")
	}
	if claims.HasScope(ScopeAgentLifecycle) {
		t.Error("expected ScopeAgentLifecycle NOT to be auto-granted without dev-auth")
	}
}
