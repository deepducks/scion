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

package sqlite

import (
	"context"
	"errors"

	"github.com/GoogleCloudPlatform/scion/pkg/store"
)

// errWorkflowRunNotInSQLite is returned when workflow run operations are called
// on the plain SQLite store. WorkflowRun is exclusively Ent-backed; these stubs
// exist only to satisfy the store.Store interface. In production, the
// entadapter.CompositeStore overrides these methods before they are reached.
var errWorkflowRunNotInSQLite = errors.New("workflow run operations require the ent store (entadapter.CompositeStore)")

// CreateWorkflowRun is a stub — overridden by the CompositeStore in production.
func (s *SQLiteStore) CreateWorkflowRun(_ context.Context, _ *store.WorkflowRun) error {
	return errWorkflowRunNotInSQLite
}

// GetWorkflowRun is a stub — overridden by the CompositeStore in production.
func (s *SQLiteStore) GetWorkflowRun(_ context.Context, _ string) (*store.WorkflowRun, error) {
	return nil, errWorkflowRunNotInSQLite
}

// ListWorkflowRuns is a stub — overridden by the CompositeStore in production.
func (s *SQLiteStore) ListWorkflowRuns(_ context.Context, _ store.WorkflowRunListOptions) (*store.ListResult[store.WorkflowRun], error) {
	return nil, errWorkflowRunNotInSQLite
}

// CancelWorkflowRun is a stub — overridden by the CompositeStore in production.
func (s *SQLiteStore) CancelWorkflowRun(_ context.Context, _ string) (*store.WorkflowRun, error) {
	return nil, errWorkflowRunNotInSQLite
}

// TransitionWorkflowRun is a stub — overridden by the CompositeStore in production.
func (s *SQLiteStore) TransitionWorkflowRun(_ context.Context, _ string, _ store.WorkflowRunTransition, _ []string) (*store.WorkflowRun, error) {
	return nil, errWorkflowRunNotInSQLite
}
