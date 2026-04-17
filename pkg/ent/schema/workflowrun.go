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

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// WorkflowRun represents a single invocation of a duckflux workflow,
// dispatched by the Hub and executed in an ephemeral container on a broker.
//
// Authorization invariant: exactly one of created_by_user_id or
// created_by_agent_id must be set. ent cannot enforce XOR at the schema level;
// this is enforced at the API handler layer.
//
// Broker invariant: there is no Broker ent entity yet. The broker identifier
// is stored as a nullable string field (broker_id) without a formal FK.
// When a RuntimeBroker entity is introduced, this field will be migrated to
// a UUID FK edge.
type WorkflowRun struct {
	ent.Schema
}

// Fields of the WorkflowRun.
func (WorkflowRun) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("grove_id", uuid.UUID{}),
		// broker_id is null while the run is queued; populated when the
		// Hub dispatcher picks a broker and transitions to provisioning.
		// Stored as a plain string (no FK) until a RuntimeBroker ent entity exists.
		field.String("broker_id").
			Optional().
			Nillable(),
		// source_yaml holds the inline duckflux workflow YAML for this run.
		// v1 supports inline only; a WorkflowDefinition registry is out of scope.
		field.Text("source_yaml"),
		// inputs_json is the JSON envelope passed to quack --input-file.
		// Null when no inputs were provided.
		field.Bytes("inputs_json").
			Optional().
			Nillable(),
		field.Enum("status").
			Values("queued", "provisioning", "running", "succeeded", "failed", "canceled", "timed_out").
			Default("queued"),
		// result_json holds the final stdout JSON from quack on succeeded.
		// May contain a {"$ref":"blob://..."} reference if the output exceeded
		// the configured inline cap and was spilled to blob storage.
		field.Bytes("result_json").
			Optional().
			Nillable(),
		// error_message is a human-readable error string populated on failed or timed_out.
		// Named error_message (not error) to avoid collision with the reserved Go identifier.
		field.String("error_message").
			Optional().
			Nillable(),
		// trace_url is the blob key or signed URL for the uploaded trace archive.
		field.String("trace_url").
			Optional().
			Nillable(),
		// started_at is set when the broker transitions the run to running.
		field.Time("started_at").
			Optional().
			Nillable(),
		// finished_at is set on any terminal state transition.
		field.Time("finished_at").
			Optional().
			Nillable(),
		// created_by_user_id is set when the run was created by a user via OAuth.
		field.UUID("created_by_user_id", uuid.UUID{}).
			Optional().
			Nillable(),
		// created_by_agent_id is set when the run was created by an agent JWT (Phase 4b).
		field.UUID("created_by_agent_id", uuid.UUID{}).
			Optional().
			Nillable(),
		field.Time("created").
			Default(time.Now).
			Immutable(),
		field.Time("updated").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the WorkflowRun.
func (WorkflowRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("grove", Grove.Type).
			Ref("workflow_runs").
			Field("grove_id").
			Required().
			Unique(),
		edge.From("created_by_user", User.Type).
			Ref("created_workflow_runs").
			Field("created_by_user_id").
			Unique(),
		edge.From("created_by_agent", Agent.Type).
			Ref("created_workflow_runs").
			Field("created_by_agent_id").
			Unique(),
	}
}

// Indexes of the WorkflowRun.
func (WorkflowRun) Indexes() []ent.Index {
	return []ent.Index{
		// Primary list-by-grove query: (grove_id, created DESC).
		index.Fields("grove_id", "created"),
		// Reaper and status-filter queries.
		index.Fields("status"),
	}
}
