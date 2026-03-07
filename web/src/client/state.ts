/**
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * Client-side state manager with view-scoped SSE subscriptions
 *
 * The StateManager uses view-scoped subscriptions: the subscription scope
 * follows navigation, not individual entities. A paginated list of 200 agents
 * uses one grove-level subscription, not 200 agent-level subscriptions.
 * Pagination is a rendering concern; the full state map is maintained in memory.
 *
 * See web-frontend-design.md §4.4 and §12.2.
 */

import { SSEClient } from './sse-client.js';
import type { SSEUpdateEvent } from './sse-client.js';
import type { Agent, Grove, RuntimeBroker } from '../shared/types.js';

/** Activities that should not be overwritten by idle/empty transitions */
const STICKY_ACTIVITIES = new Set(['waiting_for_input', 'completed', 'limits_exceeded']);

/** Subscription scope matches view context */
export type ViewScope =
  | { type: 'dashboard' }
  | { type: 'grove'; groveId: string }
  | { type: 'agent-detail'; groveId: string; agentId: string }
  | { type: 'brokers-list' }
  | { type: 'broker-detail'; brokerId: string };

/** Full in-memory state for the current scope */
export interface AppState {
  agents: Map<string, Agent>;
  groves: Map<string, Grove>;
  brokers: Map<string, RuntimeBroker>;
  deletedGroveIds: Set<string>;
  deletedAgentIds: Set<string>;
  connected: boolean;
  scope: ViewScope | null;
  /** Scope-level capabilities from the SSR-prefetched list response */
  scopeCapabilities: import('../shared/types.js').Capabilities | undefined;
}

/** Events dispatched by StateManager */
export type StateEventType =
  | 'agents-updated'
  | 'groves-updated'
  | 'brokers-updated'
  | 'connected'
  | 'disconnected'
  | 'scope-changed';

export class StateManager extends EventTarget {
  private state: AppState = {
    agents: new Map(),
    groves: new Map(),
    brokers: new Map(),
    deletedGroveIds: new Set(),
    deletedAgentIds: new Set(),
    connected: false,
    scope: null,
    scopeCapabilities: undefined,
  };

  private sseClient = new SSEClient();

  constructor() {
    super();

    // Wire SSE client events to state management
    this.sseClient.addEventListener('update', ((event: CustomEvent<SSEUpdateEvent>) => {
      this.handleUpdate(event.detail);
    }) as EventListener);

    this.sseClient.addEventListener('connected', () => {
      this.state.connected = true;
      this.notify('connected');
    });

    this.sseClient.addEventListener('disconnected', () => {
      this.state.connected = false;
      this.notify('disconnected');
    });
  }

  /**
   * Initialize state from server-rendered data.
   * Called once on page load with the __SCION_DATA__ payload.
   *
   * @param initialData - Agents and/or groves from the prefetched API response.
   * @param scopeCapabilities - Scope-level capabilities from the API response's
   *   top-level `_capabilities` field (if present).
   */
  hydrate(
    initialData: { agents?: Agent[]; groves?: Grove[] },
    scopeCapabilities?: import('../shared/types.js').Capabilities,
  ): void {
    if (initialData.agents) {
      for (const agent of initialData.agents) {
        this.state.agents.set(agent.id, agent);
      }
    }

    if (initialData.groves) {
      for (const grove of initialData.groves) {
        this.state.groves.set(grove.id, grove);
      }
    }

    if (scopeCapabilities) {
      this.state.scopeCapabilities = scopeCapabilities;
    }
  }

  /**
   * Set the view scope. Closes any existing SSE connection and opens
   * a new one with subjects matching the view context.
   * Called by the router on navigation.
   */
  setScope(scope: ViewScope): void {
    // Skip if scope is unchanged
    if (this.state.scope && this.scopeEquals(this.state.scope, scope)) {
      return;
    }

    this.state.scope = scope;

    // Clear state from previous scope
    this.state.agents.clear();
    this.state.groves.clear();
    this.state.brokers.clear();
    this.state.deletedGroveIds.clear();
    this.state.deletedAgentIds.clear();
    this.state.scopeCapabilities = undefined;

    const subjects = this.subjectsForScope(scope);
    if (subjects.length > 0) {
      this.sseClient.connect(subjects);
    }

    this.notify('scope-changed');
  }

  /**
   * Map view scope to event subject patterns.
   * Matches the subscription tiers defined in §12.2.
   */
  private subjectsForScope(scope: ViewScope): string[] {
    switch (scope.type) {
      case 'dashboard':
        // All grove-scoped events: lifecycle (created/updated/deleted) and agent changes
        return ['grove.>'];

      case 'grove':
        // Grove-level wildcard: all lightweight/medium events for agents in this grove
        return [`grove.${scope.groveId}.>`];

      case 'agent-detail':
        // Keep grove subscription for breadcrumb/sidebar freshness.
        // Add agent-specific subscription for heavy events (harness output).
        return [`grove.${scope.groveId}.>`, `agent.${scope.agentId}.>`];

      case 'brokers-list':
        // All broker-scoped events: status changes
        return ['broker.>'];

      case 'broker-detail':
        // Broker-specific events
        return ['broker.>'];
    }
  }

  private scopeEquals(a: ViewScope, b: ViewScope): boolean {
    if (a.type !== b.type) return false;
    if (a.type === 'dashboard' && b.type === 'dashboard') return true;
    if (a.type === 'brokers-list' && b.type === 'brokers-list') return true;
    if (a.type === 'broker-detail' && b.type === 'broker-detail') return a.brokerId === b.brokerId;
    if (a.type === 'grove' && b.type === 'grove') return a.groveId === b.groveId;
    if (a.type === 'agent-detail' && b.type === 'agent-detail') {
      return a.groveId === b.groveId && a.agentId === b.agentId;
    }
    return false;
  }

  /**
   * Handle delta updates from SSE.
   * The server sends events with structure: { subject: string, data: unknown }
   * Subject format follows the event schema in §12.3.
   */
  private handleUpdate(update: SSEUpdateEvent): void {
    const { subject, data } = update;
    const parts = subject.split('.');

    // Broker-scoped events: broker.{brokerId}.{eventType}
    if (parts[0] === 'broker' && parts.length >= 3) {
      const brokerId = parts[1];
      const eventType = parts[2];
      this.handleBrokerEvent(brokerId, eventType, data);
      return;
    }

    // Agent-scoped events: agent.{agentId}.{eventType}
    if (parts[0] === 'agent' && parts.length >= 3) {
      const agentId = parts[1];
      const eventType = parts[2];
      this.handleAgentEvent(agentId, eventType, data);
      return;
    }

    // Grove-scoped events
    if (parts[0] === 'grove' && parts.length >= 3) {
      const groveId = parts[1];

      // Grove agent events: grove.{groveId}.agent.{eventType}
      if (parts[2] === 'agent' && parts.length >= 4) {
        const eventType = parts[3];
        const agentData = data as Record<string, unknown>;
        const agentId = agentData.agentId as string;
        if (agentId) {
          this.handleAgentEvent(agentId, eventType, data);
        }
        return;
      }

      // Grove broker events: grove.{groveId}.broker.{eventType}
      if (parts[2] === 'broker') {
        // Broker events don't affect agent/grove state maps currently
        return;
      }

      // Grove metadata events: grove.{groveId}.updated or grove.*.summary
      this.handleGroveEvent(groveId, parts[2], data);
    }
  }

  private handleAgentEvent(agentId: string, eventType: string, data: unknown): void {
    if (eventType === 'deleted') {
      this.state.agents.delete(agentId);
      this.state.deletedAgentIds.add(agentId);
    } else {
      // Merge delta into existing agent state
      const existing = this.state.agents.get(agentId) || ({} as Agent);
      const delta = data as Partial<Agent>;
      // Preserve sticky activities: if the incoming activity is idle/empty
      // but the existing activity is sticky, keep the existing value.
      const incomingActivity = delta.activity as string | undefined;
      if (
        incomingActivity !== undefined &&
        (incomingActivity === 'idle' || incomingActivity === '') &&
        existing.activity &&
        STICKY_ACTIVITIES.has(existing.activity)
      ) {
        delete delta.activity;
      }
      // Promote limits tracking fields from SSE detail to top-level agent
      const detail = delta.detail as import('../shared/types.js').AgentDetail | undefined;
      if (detail) {
        if (detail.currentTurns !== undefined) {
          (delta as Record<string, unknown>).currentTurns = detail.currentTurns;
        }
        if (detail.currentModelCalls !== undefined) {
          (delta as Record<string, unknown>).currentModelCalls = detail.currentModelCalls;
        }
        if (detail.startedAt) {
          (delta as Record<string, unknown>).startedAt = detail.startedAt;
        }
      }
      // Ensure id is always set
      const updated = { ...existing, ...delta, id: agentId };
      // Preserve _capabilities from existing state when the delta doesn't
      // provide valid capabilities (SSE status deltas typically omit them).
      if (!delta._capabilities && existing._capabilities) {
        updated._capabilities = existing._capabilities;
      }
      this.state.agents.set(agentId, updated as Agent);
    }
    this.notify('agents-updated');
  }

  private handleGroveEvent(groveId: string, eventType: string, data: unknown): void {
    if (eventType === 'deleted') {
      this.state.groves.delete(groveId);
      this.state.deletedGroveIds.add(groveId);
    } else if (eventType === 'summary') {
      // Dashboard summary event: grove.*.summary
      const summaryData = data as Partial<Grove> & { groveId?: string };
      const id = summaryData.groveId || groveId;
      const existing = this.state.groves.get(id) || ({} as Grove);
      const updated = { ...existing, ...summaryData, id };
      if (!summaryData._capabilities && existing._capabilities) {
        updated._capabilities = existing._capabilities;
      }
      this.state.groves.set(id, updated as Grove);
    } else {
      // Grove lifecycle events: created, updated
      const groveData = data as Partial<Grove> & { groveId?: string };
      const id = groveData.groveId || groveId;
      const existing = this.state.groves.get(id) || ({} as Grove);
      const updated = { ...existing, ...groveData, id };
      if (!groveData._capabilities && existing._capabilities) {
        updated._capabilities = existing._capabilities;
      }
      this.state.groves.set(id, updated as Grove);
    }
    this.notify('groves-updated');
  }

  private handleBrokerEvent(brokerId: string, eventType: string, data: unknown): void {
    if (eventType === 'deleted') {
      this.state.brokers.delete(brokerId);
    } else {
      // Merge delta into existing broker state
      const existing = this.state.brokers.get(brokerId) || ({} as RuntimeBroker);
      const delta = data as Partial<RuntimeBroker>;
      // Map brokerId field from event payload to id
      const id = (delta as Record<string, unknown>).brokerId as string || brokerId;
      const updated = { ...existing, ...delta, id };
      this.state.brokers.set(id, updated as RuntimeBroker);
    }
    this.notify('brokers-updated');
  }

  private notify(event: StateEventType): void {
    this.dispatchEvent(new CustomEvent(event, { detail: this.state }));
  }

  /**
   * Seed the agents map with full objects from a REST API response.
   * Called after initial data fetch so that SSE delta merging has
   * complete baseline data. Does not trigger notifications — the
   * calling component already holds the data from its own fetch.
   */
  seedAgents(agents: Agent[]): void {
    for (const agent of agents) {
      this.state.agents.set(agent.id, agent);
    }
  }

  /**
   * Seed the groves map with full objects from a REST API response.
   */
  seedGroves(groves: Grove[]): void {
    for (const grove of groves) {
      this.state.groves.set(grove.id, grove);
    }
  }

  /**
   * Seed the brokers map with full objects from a REST API response.
   */
  seedBrokers(brokers: RuntimeBroker[]): void {
    for (const broker of brokers) {
      this.state.brokers.set(broker.id, broker);
    }
  }

  /** Expose the SSE client for debug instrumentation */
  get sseClientInstance(): SSEClient {
    return this.sseClient;
  }

  /** Current subscription subjects from the SSE client */
  get currentSubjects(): string[] {
    return this.sseClient.currentSubjects;
  }

  /** Snapshot of current state for debug display */
  getStateSnapshot(): {
    agentCount: number;
    groveCount: number;
    brokerCount: number;
    agentIds: string[];
    groveIds: string[];
    brokerIds: string[];
    deletedGroveIds: string[];
    deletedAgentIds: string[];
  } {
    return {
      agentCount: this.state.agents.size,
      groveCount: this.state.groves.size,
      brokerCount: this.state.brokers.size,
      agentIds: Array.from(this.state.agents.keys()),
      groveIds: Array.from(this.state.groves.keys()),
      brokerIds: Array.from(this.state.brokers.keys()),
      deletedGroveIds: Array.from(this.state.deletedGroveIds),
      deletedAgentIds: Array.from(this.state.deletedAgentIds),
    };
  }

  /** Disconnect the SSE connection. Called on page unload. */
  disconnect(): void {
    this.sseClient.disconnect();
    this.state.connected = false;
  }

  // --- Getters ---
  // The full state map is maintained regardless of pagination.
  // Components render the slice they need.

  getAgents(): Agent[] {
    return Array.from(this.state.agents.values());
  }

  getAgent(id: string): Agent | undefined {
    return this.state.agents.get(id);
  }

  getGroves(): Grove[] {
    return Array.from(this.state.groves.values());
  }

  getGrove(id: string): Grove | undefined {
    return this.state.groves.get(id);
  }

  getBrokers(): RuntimeBroker[] {
    return Array.from(this.state.brokers.values());
  }

  getBroker(id: string): RuntimeBroker | undefined {
    return this.state.brokers.get(id);
  }

  getDeletedGroveIds(): Set<string> {
    return this.state.deletedGroveIds;
  }

  getDeletedAgentIds(): Set<string> {
    return this.state.deletedAgentIds;
  }

  getScopeCapabilities(): import('../shared/types.js').Capabilities | undefined {
    return this.state.scopeCapabilities;
  }

  get isConnected(): boolean {
    return this.state.connected;
  }

  get currentScope(): ViewScope | null {
    return this.state.scope;
  }
}

/** Singleton instance — accessed via import */
export const stateManager = new StateManager();
