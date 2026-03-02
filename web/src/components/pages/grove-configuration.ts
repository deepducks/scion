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
 * Grove Configuration page
 *
 * Manages grove-scoped environment variables and secrets with full CRUD lifecycle.
 * Combines both sections on a single page accessed from the grove detail header.
 */

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import type {
  PageData,
  Grove,
  EnvVar,
  Secret,
  SecretType,
  InjectionMode,
} from '../../shared/types.js';
import { apiFetch } from '../../client/api.js';

@customElement('scion-page-grove-configuration')
export class ScionPageGroveConfiguration extends LitElement {
  @property({ type: Object })
  pageData: PageData | null = null;

  @property({ type: String })
  groveId = '';

  // Page state
  @state() private loading = true;
  @state() private grove: Grove | null = null;
  @state() private error: string | null = null;

  // Env vars
  @state() private envVars: EnvVar[] = [];
  @state() private envLoading = false;
  @state() private envError: string | null = null;

  // Env var dialog
  @state() private envDialogOpen = false;
  @state() private envDialogMode: 'create' | 'edit' = 'create';
  @state() private envDialogKey = '';
  @state() private envDialogValue = '';
  @state() private envDialogDescription = '';
  @state() private envDialogSensitive = false;
  @state() private envDialogSecret = false;
  @state() private envDialogInjectionMode: InjectionMode = 'always';
  @state() private envDialogLoading = false;
  @state() private envDialogError: string | null = null;
  @state() private envDeletingKey: string | null = null;

  // Secrets
  @state() private secrets: Secret[] = [];
  @state() private secretsLoading = false;
  @state() private secretsError: string | null = null;

  // Secret dialog
  @state() private secretDialogOpen = false;
  @state() private secretDialogMode: 'create' | 'update' = 'create';
  @state() private secretDialogKey = '';
  @state() private secretDialogValue = '';
  @state() private secretDialogDescription = '';
  @state() private secretDialogType: SecretType = 'environment';
  @state() private secretDialogTarget = '';
  @state() private secretDialogInjectionMode: InjectionMode = 'always';
  @state() private secretDialogLoading = false;
  @state() private secretDialogError: string | null = null;
  @state() private secretDeletingKey: string | null = null;

  static override styles = css`
    :host {
      display: block;
    }

    .back-link {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      color: var(--scion-text-muted, #64748b);
      text-decoration: none;
      font-size: 0.875rem;
      margin-bottom: 1rem;
    }

    .back-link:hover {
      color: var(--scion-primary, #3b82f6);
    }

    .header {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      margin-bottom: 2rem;
    }

    .header sl-icon {
      color: var(--scion-primary, #3b82f6);
      font-size: 1.5rem;
    }

    .header h1 {
      font-size: 1.5rem;
      font-weight: 700;
      color: var(--scion-text, #1e293b);
      margin: 0;
    }

    .section {
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
      padding: 1.5rem;
      margin-bottom: 1.5rem;
    }

    .section-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      margin-bottom: 1rem;
      gap: 1rem;
    }

    .section-header-info h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.25rem 0;
    }

    .section-header-info p {
      color: var(--scion-text-muted, #64748b);
      font-size: 0.875rem;
      margin: 0;
    }

    .table-container {
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
      overflow: hidden;
    }

    table {
      width: 100%;
      border-collapse: collapse;
    }

    th {
      text-align: left;
      padding: 0.75rem 1rem;
      font-size: 0.75rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--scion-text-muted, #64748b);
      background: var(--scion-bg-subtle, #f1f5f9);
      border-bottom: 1px solid var(--scion-border, #e2e8f0);
    }

    td {
      padding: 0.75rem 1rem;
      font-size: 0.875rem;
      color: var(--scion-text, #1e293b);
      border-bottom: 1px solid var(--scion-border, #e2e8f0);
      vertical-align: middle;
    }

    tr:last-child td {
      border-bottom: none;
    }

    tr:hover td {
      background: var(--scion-bg-subtle, #f1f5f9);
    }

    .key-cell {
      font-family: var(--scion-font-mono, monospace);
      font-weight: 600;
      font-size: 0.8125rem;
    }

    .value-cell {
      font-family: var(--scion-font-mono, monospace);
      font-size: 0.8125rem;
      max-width: 300px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .description-cell {
      max-width: 200px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--scion-text-muted, #64748b);
    }

    .badge {
      display: inline-flex;
      align-items: center;
      gap: 0.25rem;
      padding: 0.125rem 0.5rem;
      border-radius: 9999px;
      font-size: 0.6875rem;
      font-weight: 500;
    }

    .badge.sensitive {
      background: var(--sl-color-warning-100, #fef3c7);
      color: var(--sl-color-warning-700, #b45309);
    }

    .badge.secret {
      background: var(--sl-color-danger-100, #fee2e2);
      color: var(--sl-color-danger-700, #b91c1c);
    }

    .badges {
      display: flex;
      gap: 0.375rem;
      flex-wrap: wrap;
    }

    .key-info {
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }

    .key-icon {
      width: 1.75rem;
      height: 1.75rem;
      border-radius: 0.375rem;
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      background: var(--sl-color-danger-100, #fee2e2);
      color: var(--sl-color-danger-600, #dc2626);
    }

    .key-icon sl-icon {
      font-size: 0.875rem;
    }

    .type-badge {
      display: inline-flex;
      align-items: center;
      padding: 0.125rem 0.5rem;
      border-radius: 9999px;
      font-size: 0.6875rem;
      font-weight: 500;
    }

    .type-badge.environment {
      background: var(--sl-color-primary-100, #dbeafe);
      color: var(--sl-color-primary-700, #1d4ed8);
    }

    .type-badge.variable {
      background: var(--sl-color-success-100, #dcfce7);
      color: var(--sl-color-success-700, #15803d);
    }

    .type-badge.file {
      background: var(--sl-color-warning-100, #fef3c7);
      color: var(--sl-color-warning-700, #b45309);
    }

    .version-badge {
      display: inline-flex;
      align-items: center;
      padding: 0.125rem 0.5rem;
      border-radius: 9999px;
      font-size: 0.6875rem;
      font-weight: 500;
      background: var(--scion-bg-subtle, #f1f5f9);
      color: var(--scion-text-muted, #64748b);
      font-family: var(--scion-font-mono, monospace);
    }

    .actions-cell {
      text-align: right;
      white-space: nowrap;
    }

    .meta-text {
      font-size: 0.8125rem;
      color: var(--scion-text-muted, #64748b);
    }

    .empty-state {
      text-align: center;
      padding: 2rem 1.5rem;
      border: 1px dashed var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
    }

    .empty-state > sl-icon {
      font-size: 2.5rem;
      color: var(--scion-text-muted, #64748b);
      opacity: 0.5;
      margin-bottom: 0.5rem;
    }

    .empty-state h3 {
      font-size: 1rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.375rem 0;
    }

    .empty-state p {
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1rem 0;
      font-size: 0.875rem;
    }

    .loading-state {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 4rem 2rem;
      color: var(--scion-text-muted, #64748b);
    }

    .loading-state sl-spinner {
      font-size: 2rem;
      margin-bottom: 1rem;
    }

    .section-loading {
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 2rem;
      color: var(--scion-text-muted, #64748b);
      gap: 0.75rem;
    }

    .error-state {
      text-align: center;
      padding: 3rem 2rem;
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--sl-color-danger-200, #fecaca);
      border-radius: var(--scion-radius-lg, 0.75rem);
    }

    .error-state sl-icon {
      font-size: 3rem;
      color: var(--sl-color-danger-500, #ef4444);
      margin-bottom: 1rem;
    }

    .error-state h2 {
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.5rem 0;
    }

    .error-state p {
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1rem 0;
    }

    .error-details {
      font-family: var(--scion-font-mono, monospace);
      font-size: 0.875rem;
      background: var(--scion-bg-subtle, #f1f5f9);
      padding: 0.75rem 1rem;
      border-radius: var(--scion-radius, 0.5rem);
      color: var(--sl-color-danger-700, #b91c1c);
      margin-bottom: 1rem;
    }

    .section-error {
      color: var(--sl-color-danger-600, #dc2626);
      font-size: 0.875rem;
      padding: 0.75rem 1rem;
      background: var(--sl-color-danger-50, #fef2f2);
      border-radius: var(--scion-radius, 0.5rem);
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 0.5rem;
    }

    .dialog-form {
      display: flex;
      flex-direction: column;
      gap: 1rem;
    }

    .dialog-error {
      color: var(--sl-color-danger-600, #dc2626);
      font-size: 0.875rem;
      padding: 0.5rem 0.75rem;
      background: var(--sl-color-danger-50, #fef2f2);
      border-radius: var(--scion-radius, 0.5rem);
    }

    .dialog-hint {
      font-size: 0.8125rem;
      color: var(--scion-text-muted, #64748b);
      padding: 0.625rem 0.75rem;
      background: var(--sl-color-warning-50, #fffbeb);
      border: 1px solid var(--sl-color-warning-200, #fde68a);
      border-radius: var(--scion-radius, 0.5rem);
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }

    .dialog-hint sl-icon {
      flex-shrink: 0;
      color: var(--sl-color-warning-600, #d97706);
    }

    .checkbox-group {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }

    .checkbox-label {
      display: flex;
      align-items: flex-start;
      gap: 0.5rem;
      cursor: pointer;
      font-size: 0.875rem;
      color: var(--scion-text, #1e293b);
    }

    .checkbox-label input[type='checkbox'] {
      margin-top: 0.125rem;
      flex-shrink: 0;
    }

    .checkbox-text {
      display: flex;
      flex-direction: column;
    }

    .checkbox-description {
      font-size: 0.75rem;
      color: var(--scion-text-muted, #64748b);
      margin-top: 0.125rem;
    }

    .radio-field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }

    .radio-field-label {
      font-size: 0.875rem;
      font-weight: 500;
      color: var(--scion-text, #1e293b);
    }

    .radio-field-help {
      font-size: 0.75rem;
      color: var(--scion-text-muted, #64748b);
    }

    .badge.inject-always {
      background: var(--sl-color-primary-100, #dbeafe);
      color: var(--sl-color-primary-700, #1d4ed8);
    }

    .badge.inject-as-needed {
      background: var(--scion-bg-subtle, #f1f5f9);
      color: var(--scion-text-muted, #64748b);
    }

    @media (max-width: 768px) {
      .hide-mobile {
        display: none;
      }
    }
  `;

  override connectedCallback(): void {
    super.connectedCallback();
    if (!this.groveId && typeof window !== 'undefined') {
      const match = window.location.pathname.match(/\/groves\/([^/]+)/);
      if (match) {
        this.groveId = match[1];
      }
    }
    void this.loadAll();
  }

  private async loadAll(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}`);
      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }
      this.grove = (await response.json()) as Grove;
    } catch (err) {
      console.error('Failed to load grove:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load grove';
      this.loading = false;
      return;
    }

    this.loading = false;

    // Load env vars and secrets in parallel (non-blocking)
    void this.loadEnvVars();
    void this.loadSecrets();
  }

  // ── Environment Variables ──────────────────────────────────────────────

  private async loadEnvVars(): Promise<void> {
    this.envLoading = true;
    this.envError = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}/env`);
      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }
      const data = (await response.json()) as { envVars?: EnvVar[] } | EnvVar[];
      this.envVars = Array.isArray(data) ? data : data.envVars || [];
    } catch (err) {
      console.error('Failed to load grove env vars:', err);
      this.envError = err instanceof Error ? err.message : 'Failed to load environment variables';
    } finally {
      this.envLoading = false;
    }
  }

  private openEnvCreateDialog(): void {
    this.envDialogMode = 'create';
    this.envDialogKey = '';
    this.envDialogValue = '';
    this.envDialogDescription = '';
    this.envDialogSensitive = false;
    this.envDialogSecret = false;
    this.envDialogInjectionMode = 'always';
    this.envDialogError = null;
    this.envDialogOpen = true;
  }

  private openEnvEditDialog(envVar: EnvVar): void {
    this.envDialogMode = 'edit';
    this.envDialogKey = envVar.key;
    this.envDialogValue = envVar.sensitive || envVar.secret ? '' : envVar.value;
    this.envDialogDescription = envVar.description || '';
    this.envDialogSensitive = envVar.sensitive;
    this.envDialogSecret = envVar.secret;
    this.envDialogInjectionMode = envVar.injectionMode || 'always';
    this.envDialogError = null;
    this.envDialogOpen = true;
  }

  private closeEnvDialog(): void {
    this.envDialogOpen = false;
  }

  private async handleEnvSave(e: Event): Promise<void> {
    e.preventDefault();

    const key = this.envDialogKey.trim();
    if (!key) {
      this.envDialogError = 'Key is required';
      return;
    }

    if (this.envDialogMode === 'create' && !this.envDialogValue) {
      this.envDialogError = 'Value is required';
      return;
    }

    this.envDialogLoading = true;
    this.envDialogError = null;

    try {
      const body: Record<string, unknown> = {
        value: this.envDialogValue,
        scope: 'grove',
        scopeId: this.groveId,
        description: this.envDialogDescription || undefined,
        sensitive: this.envDialogSensitive,
        secret: this.envDialogSecret,
        injectionMode: this.envDialogInjectionMode,
      };

      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/env/${encodeURIComponent(key)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        }
      );

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      this.closeEnvDialog();
      await this.loadEnvVars();
    } catch (err) {
      console.error('Failed to save env var:', err);
      this.envDialogError = err instanceof Error ? err.message : 'Failed to save';
    } finally {
      this.envDialogLoading = false;
    }
  }

  private async handleEnvDelete(envVar: EnvVar): Promise<void> {
    if (!confirm(`Delete environment variable "${envVar.key}"? This cannot be undone.`)) {
      return;
    }

    this.envDeletingKey = envVar.key;

    try {
      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/env/${encodeURIComponent(envVar.key)}`,
        { method: 'DELETE' }
      );

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete (HTTP ${response.status})`);
      }

      await this.loadEnvVars();
    } catch (err) {
      console.error('Failed to delete env var:', err);
      alert(err instanceof Error ? err.message : 'Failed to delete');
    } finally {
      this.envDeletingKey = null;
    }
  }

  // ── Secrets ────────────────────────────────────────────────────────────

  private async loadSecrets(): Promise<void> {
    this.secretsLoading = true;
    this.secretsError = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}/secrets`);
      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }
      const data = (await response.json()) as { secrets?: Secret[] } | Secret[];
      this.secrets = Array.isArray(data) ? data : data.secrets || [];
    } catch (err) {
      console.error('Failed to load grove secrets:', err);
      this.secretsError = err instanceof Error ? err.message : 'Failed to load secrets';
    } finally {
      this.secretsLoading = false;
    }
  }

  private openSecretCreateDialog(): void {
    this.secretDialogMode = 'create';
    this.secretDialogKey = '';
    this.secretDialogValue = '';
    this.secretDialogDescription = '';
    this.secretDialogType = 'environment';
    this.secretDialogTarget = '';
    this.secretDialogInjectionMode = 'always';
    this.secretDialogError = null;
    this.secretDialogOpen = true;
  }

  private openSecretUpdateDialog(secret: Secret): void {
    this.secretDialogMode = 'update';
    this.secretDialogKey = secret.key;
    this.secretDialogValue = '';
    this.secretDialogDescription = secret.description || '';
    this.secretDialogType = secret.type;
    this.secretDialogTarget = secret.target || '';
    this.secretDialogInjectionMode = secret.injectionMode || 'always';
    this.secretDialogError = null;
    this.secretDialogOpen = true;
  }

  private closeSecretDialog(): void {
    this.secretDialogOpen = false;
  }

  private async handleSecretSave(e: Event): Promise<void> {
    e.preventDefault();

    const key = this.secretDialogKey.trim();
    if (!key) {
      this.secretDialogError = 'Key is required';
      return;
    }

    if (!this.secretDialogValue) {
      this.secretDialogError = 'Value is required';
      return;
    }

    this.secretDialogLoading = true;
    this.secretDialogError = null;

    try {
      const body: Record<string, unknown> = {
        value: this.secretDialogValue,
        scope: 'grove',
        scopeId: this.groveId,
        description: this.secretDialogDescription || undefined,
        type: this.secretDialogType,
        target: this.secretDialogTarget || undefined,
        injectionMode: this.secretDialogInjectionMode,
      };

      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/secrets/${encodeURIComponent(key)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        }
      );

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      this.closeSecretDialog();
      await this.loadSecrets();
    } catch (err) {
      console.error('Failed to save secret:', err);
      this.secretDialogError = err instanceof Error ? err.message : 'Failed to save';
    } finally {
      this.secretDialogLoading = false;
    }
  }

  private async handleSecretDelete(secret: Secret): Promise<void> {
    if (!confirm(`Delete secret "${secret.key}"? This cannot be undone.`)) {
      return;
    }

    this.secretDeletingKey = secret.key;

    try {
      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/secrets/${encodeURIComponent(secret.key)}`,
        { method: 'DELETE' }
      );

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete (HTTP ${response.status})`);
      }

      await this.loadSecrets();
    } catch (err) {
      console.error('Failed to delete secret:', err);
      alert(err instanceof Error ? err.message : 'Failed to delete');
    } finally {
      this.secretDeletingKey = null;
    }
  }

  // ── Utilities ──────────────────────────────────────────────────────────

  private formatRelativeTime(dateString: string): string {
    try {
      const date = new Date(dateString);
      if (isNaN(date.getTime())) return dateString;
      const diffMs = Date.now() - date.getTime();
      const diffSeconds = Math.round(diffMs / 1000);
      const diffMinutes = Math.round(diffMs / (1000 * 60));
      const diffHours = Math.round(diffMs / (1000 * 60 * 60));
      const diffDays = Math.round(diffMs / (1000 * 60 * 60 * 24));

      const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

      if (Math.abs(diffSeconds) < 60) {
        return rtf.format(-diffSeconds, 'second');
      } else if (Math.abs(diffMinutes) < 60) {
        return rtf.format(-diffMinutes, 'minute');
      } else if (Math.abs(diffHours) < 24) {
        return rtf.format(-diffHours, 'hour');
      } else {
        return rtf.format(-diffDays, 'day');
      }
    } catch {
      return dateString;
    }
  }

  // ── Rendering ──────────────────────────────────────────────────────────

  override render() {
    if (this.loading) {
      return this.renderLoading();
    }

    if (this.error || !this.grove) {
      return this.renderError();
    }

    return html`
      <a href="/groves/${this.groveId}" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Grove
      </a>

      <div class="header">
        <sl-icon name="sliders"></sl-icon>
        <h1>${this.grove.name} Configuration</h1>
      </div>

      ${this.renderEnvVarsSection()} ${this.renderSecretsSection()} ${this.renderEnvDialog()}
      ${this.renderSecretDialog()}
    `;
  }

  // ── Env Vars Section ───────────────────────────────────────────────────

  private renderEnvVarsSection() {
    return html`
      <div class="section">
        <div class="section-header">
          <div class="section-header-info">
            <h2>Environment Variables</h2>
            <p>Manage environment variables injected into agents in this grove at runtime.</p>
          </div>
          <sl-button variant="primary" size="small" @click=${this.openEnvCreateDialog}>
            <sl-icon slot="prefix" name="plus-lg"></sl-icon>
            Add Variable
          </sl-button>
        </div>

        ${this.envLoading
          ? html`<div class="section-loading">
              <sl-spinner></sl-spinner> Loading environment variables...
            </div>`
          : this.envError
            ? html`<div class="section-error">
                <span>${this.envError}</span>
                <sl-button size="small" @click=${() => this.loadEnvVars()}>
                  <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
                  Retry
                </sl-button>
              </div>`
            : this.envVars.length === 0
              ? this.renderEnvEmpty()
              : this.renderEnvTable()}
      </div>
    `;
  }

  private renderEnvTable() {
    return html`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th>Value</th>
              <th class="hide-mobile">Description</th>
              <th>Inject</th>
              <th>Flags</th>
              <th class="hide-mobile">Updated</th>
              <th class="actions-cell"></th>
            </tr>
          </thead>
          <tbody>
            ${this.envVars.map((envVar) => this.renderEnvRow(envVar))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderEnvRow(envVar: EnvVar) {
    const isDeleting = this.envDeletingKey === envVar.key;
    const displayValue =
      envVar.secret || envVar.sensitive
        ? '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022'
        : envVar.value;

    return html`
      <tr>
        <td class="key-cell">${envVar.key}</td>
        <td class="value-cell">${displayValue}</td>
        <td class="description-cell hide-mobile">${envVar.description || '\u2014'}</td>
        <td>
          ${envVar.injectionMode === 'as_needed'
            ? html`<span class="badge inject-as-needed">as needed</span>`
            : html`<span class="badge inject-always">always</span>`}
        </td>
        <td>
          <div class="badges">
            ${envVar.sensitive
              ? html`<span class="badge sensitive">
                  <sl-icon name="eye-slash" style="font-size: 0.625rem;"></sl-icon>
                  sensitive
                </span>`
              : nothing}
            ${envVar.secret
              ? html`<span class="badge secret">
                  <sl-icon name="shield-lock" style="font-size: 0.625rem;"></sl-icon>
                  secret
                </span>`
              : nothing}
          </div>
        </td>
        <td class="hide-mobile">
          <span class="meta-text">${this.formatRelativeTime(envVar.updated)}</span>
        </td>
        <td class="actions-cell">
          <sl-icon-button
            name="pencil"
            label="Edit"
            ?disabled=${isDeleting}
            @click=${() => this.openEnvEditDialog(envVar)}
          ></sl-icon-button>
          <sl-icon-button
            name="trash"
            label="Delete"
            ?disabled=${isDeleting}
            @click=${() => this.handleEnvDelete(envVar)}
          ></sl-icon-button>
        </td>
      </tr>
    `;
  }

  private renderEnvEmpty() {
    return html`
      <div class="empty-state">
        <sl-icon name="terminal"></sl-icon>
        <h3>No Environment Variables</h3>
        <p>Add environment variables that will be injected into agents in this grove.</p>
        <sl-button variant="primary" size="small" @click=${this.openEnvCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Variable
        </sl-button>
      </div>
    `;
  }

  private renderEnvDialog() {
    const title =
      this.envDialogMode === 'create' ? 'Add Environment Variable' : 'Edit Environment Variable';
    const isCreate = this.envDialogMode === 'create';

    return html`
      <sl-dialog
        label=${title}
        ?open=${this.envDialogOpen}
        @sl-request-close=${this.closeEnvDialog}
      >
        <form class="dialog-form" @submit=${this.handleEnvSave}>
          <sl-input
            label="Key"
            placeholder="e.g. API_TOKEN"
            value=${this.envDialogKey}
            ?disabled=${!isCreate}
            @sl-input=${(e: Event) => {
              this.envDialogKey = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="Value"
            placeholder=${this.envDialogMode === 'edit' &&
            (this.envDialogSensitive || this.envDialogSecret)
              ? 'Enter new value to update'
              : 'Variable value'}
            value=${this.envDialogValue}
            type=${this.envDialogSecret || this.envDialogSensitive ? 'password' : 'text'}
            @sl-input=${(e: Event) => {
              this.envDialogValue = (e.target as HTMLInputElement).value;
            }}
            ?required=${isCreate}
          ></sl-input>

          <sl-textarea
            label="Description"
            placeholder="Optional description"
            value=${this.envDialogDescription}
            rows="2"
            resize="none"
            @sl-input=${(e: Event) => {
              this.envDialogDescription = (e.target as HTMLTextAreaElement).value;
            }}
          ></sl-textarea>

          <div class="radio-field">
            <span class="radio-field-label">Inject</span>
            <sl-radio-group
              value=${this.envDialogInjectionMode}
              @sl-change=${(e: Event) => {
                this.envDialogInjectionMode = (e.target as HTMLInputElement).value as InjectionMode;
              }}
            >
              <sl-radio-button value="always">Always</sl-radio-button>
              <sl-radio-button value="as_needed">As needed</sl-radio-button>
            </sl-radio-group>
            <span class="radio-field-help">
              "As needed" injects only when the agent configuration requests this value.
            </span>
          </div>

          <div class="checkbox-group">
            <label class="checkbox-label">
              <input
                type="checkbox"
                .checked=${this.envDialogSensitive}
                @change=${(e: Event) => {
                  this.envDialogSensitive = (e.target as HTMLInputElement).checked;
                }}
              />
              <span class="checkbox-text">
                <span>Sensitive</span>
                <span class="checkbox-description"> Mask value in API responses and UI </span>
              </span>
            </label>

            <label class="checkbox-label">
              <input
                type="checkbox"
                .checked=${this.envDialogSecret}
                @change=${(e: Event) => {
                  this.envDialogSecret = (e.target as HTMLInputElement).checked;
                  if (this.envDialogSecret) {
                    this.envDialogSensitive = true;
                  }
                }}
              />
              <span class="checkbox-text">
                <span>Store as Secret</span>
                <span class="checkbox-description">
                  Encrypt and store securely. Value will never be readable after saving.
                </span>
              </span>
            </label>
          </div>

          ${this.envDialogError
            ? html`<div class="dialog-error">${this.envDialogError}</div>`
            : nothing}
        </form>

        <sl-button
          slot="footer"
          variant="default"
          @click=${this.closeEnvDialog}
          ?disabled=${this.envDialogLoading}
        >
          Cancel
        </sl-button>
        <sl-button
          slot="footer"
          variant="primary"
          ?loading=${this.envDialogLoading}
          ?disabled=${this.envDialogLoading}
          @click=${this.handleEnvSave}
        >
          ${isCreate ? 'Create' : 'Update'}
        </sl-button>
      </sl-dialog>
    `;
  }

  // ── Secrets Section ────────────────────────────────────────────────────

  private renderSecretsSection() {
    return html`
      <div class="section">
        <div class="section-header">
          <div class="section-header-info">
            <h2>Secrets</h2>
            <p>
              Manage encrypted secrets for agents in this grove. Values are write-only and cannot be
              retrieved after saving.
            </p>
          </div>
          <sl-button variant="primary" size="small" @click=${this.openSecretCreateDialog}>
            <sl-icon slot="prefix" name="plus-lg"></sl-icon>
            Add Secret
          </sl-button>
        </div>

        ${this.secretsLoading
          ? html`<div class="section-loading"><sl-spinner></sl-spinner> Loading secrets...</div>`
          : this.secretsError
            ? html`<div class="section-error">
                <span>${this.secretsError}</span>
                <sl-button size="small" @click=${() => this.loadSecrets()}>
                  <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
                  Retry
                </sl-button>
              </div>`
            : this.secrets.length === 0
              ? this.renderSecretsEmpty()
              : this.renderSecretsTable()}
      </div>
    `;
  }

  private renderSecretsTable() {
    return html`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th>Type</th>
              <th>Inject</th>
              <th class="hide-mobile">Description</th>
              <th>Version</th>
              <th class="hide-mobile">Updated</th>
              <th class="actions-cell"></th>
            </tr>
          </thead>
          <tbody>
            ${this.secrets.map((secret) => this.renderSecretRow(secret))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderSecretRow(secret: Secret) {
    const isDeleting = this.secretDeletingKey === secret.key;

    return html`
      <tr>
        <td class="key-cell">
          <div class="key-info">
            <div class="key-icon">
              <sl-icon name="shield-lock"></sl-icon>
            </div>
            ${secret.key}
          </div>
        </td>
        <td>
          <span class="type-badge ${secret.type}">${secret.type}</span>
        </td>
        <td>
          ${secret.injectionMode === 'as_needed'
            ? html`<span class="badge inject-as-needed">as needed</span>`
            : html`<span class="badge inject-always">always</span>`}
        </td>
        <td class="description-cell hide-mobile">${secret.description || '\u2014'}</td>
        <td>
          <span class="version-badge">v${secret.version}</span>
        </td>
        <td class="hide-mobile">
          <span class="meta-text">${this.formatRelativeTime(secret.updated)}</span>
        </td>
        <td class="actions-cell">
          <sl-icon-button
            name="arrow-clockwise"
            label="Update value"
            ?disabled=${isDeleting}
            @click=${() => this.openSecretUpdateDialog(secret)}
          ></sl-icon-button>
          <sl-icon-button
            name="trash"
            label="Delete"
            ?disabled=${isDeleting}
            @click=${() => this.handleSecretDelete(secret)}
          ></sl-icon-button>
        </td>
      </tr>
    `;
  }

  private renderSecretsEmpty() {
    return html`
      <div class="empty-state">
        <sl-icon name="shield-lock"></sl-icon>
        <h3>No Secrets</h3>
        <p>Add encrypted secrets that will be securely injected into agents in this grove.</p>
        <sl-button variant="primary" size="small" @click=${this.openSecretCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Secret
        </sl-button>
      </div>
    `;
  }

  private renderSecretDialog() {
    const isCreate = this.secretDialogMode === 'create';
    const title = isCreate ? 'Add Secret' : 'Update Secret';

    return html`
      <sl-dialog
        label=${title}
        ?open=${this.secretDialogOpen}
        @sl-request-close=${this.closeSecretDialog}
      >
        <form class="dialog-form" @submit=${this.handleSecretSave}>
          <sl-input
            label=${this.secretDialogType === 'file' ? 'Name' : 'Key'}
            placeholder=${this.secretDialogType === 'file'
              ? 'e.g. ssh_deploy_key'
              : 'e.g. GITHUB_TOKEN'}
            value=${this.secretDialogKey}
            ?disabled=${!isCreate}
            @sl-input=${(e: Event) => {
              this.secretDialogKey = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="Value"
            placeholder="Secret value"
            value=${this.secretDialogValue}
            type="password"
            @sl-input=${(e: Event) => {
              this.secretDialogValue = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <div class="dialog-hint">
            <sl-icon name="info-circle"></sl-icon>
            Secret values are encrypted and can never be retrieved after saving.
          </div>

          <sl-select
            label="Type"
            value=${this.secretDialogType}
            @sl-change=${(e: Event) => {
              this.secretDialogType = (e.target as HTMLSelectElement).value as SecretType;
            }}
          >
            <sl-option value="environment">Environment Variable</sl-option>
            <sl-option value="variable">Runtime Variable</sl-option>
            <sl-option value="file">File</sl-option>
          </sl-select>

          ${this.secretDialogType === 'file'
            ? html`
                <sl-input
                  label="Target Path"
                  placeholder="e.g. /home/agent/.ssh/id_rsa"
                  value=${this.secretDialogTarget}
                  @sl-input=${(e: Event) => {
                    this.secretDialogTarget = (e.target as HTMLInputElement).value;
                  }}
                ></sl-input>
              `
            : nothing}

          <sl-textarea
            label="Description"
            placeholder="Optional description"
            value=${this.secretDialogDescription}
            rows="2"
            resize="none"
            @sl-input=${(e: Event) => {
              this.secretDialogDescription = (e.target as HTMLTextAreaElement).value;
            }}
          ></sl-textarea>

          <div class="radio-field">
            <span class="radio-field-label">Inject</span>
            <sl-radio-group
              value=${this.secretDialogInjectionMode}
              @sl-change=${(e: Event) => {
                this.secretDialogInjectionMode = (e.target as HTMLInputElement)
                  .value as InjectionMode;
              }}
            >
              <sl-radio-button value="always">Always</sl-radio-button>
              <sl-radio-button value="as_needed">As needed</sl-radio-button>
            </sl-radio-group>
            <span class="radio-field-help">
              "As needed" injects only when the agent configuration requests this value.
            </span>
          </div>

          ${this.secretDialogError
            ? html`<div class="dialog-error">${this.secretDialogError}</div>`
            : nothing}
        </form>

        <sl-button
          slot="footer"
          variant="default"
          @click=${this.closeSecretDialog}
          ?disabled=${this.secretDialogLoading}
        >
          Cancel
        </sl-button>
        <sl-button
          slot="footer"
          variant="primary"
          ?loading=${this.secretDialogLoading}
          ?disabled=${this.secretDialogLoading}
          @click=${this.handleSecretSave}
        >
          ${isCreate ? 'Create' : 'Update'}
        </sl-button>
      </sl-dialog>
    `;
  }

  // ── Page-level states ──────────────────────────────────────────────────

  private renderLoading() {
    return html`
      <div class="loading-state">
        <sl-spinner></sl-spinner>
        <p>Loading configuration...</p>
      </div>
    `;
  }

  private renderError() {
    return html`
      <a href="/groves/${this.groveId}" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Grove
      </a>

      <div class="error-state">
        <sl-icon name="exclamation-triangle"></sl-icon>
        <h2>Failed to Load Configuration</h2>
        <p>There was a problem loading this grove.</p>
        <div class="error-details">${this.error || 'Grove not found'}</div>
        <sl-button variant="primary" @click=${() => this.loadAll()}>
          <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
          Retry
        </sl-button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-page-grove-configuration': ScionPageGroveConfiguration;
  }
}
