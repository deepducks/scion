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
 * Profile Environment Variables page
 *
 * Manages user-scoped environment variables with full CRUD lifecycle.
 * Supports creating env vars with an optional "store as secret" promotion.
 */

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import type { EnvVar, InjectionMode } from '../../shared/types.js';

@customElement('scion-page-profile-env-vars')
export class ScionPageProfileEnvVars extends LitElement {
  @state() private loading = true;
  @state() private envVars: EnvVar[] = [];
  @state() private error: string | null = null;

  // Create/Edit dialog
  @state() private dialogOpen = false;
  @state() private dialogMode: 'create' | 'edit' = 'create';
  @state() private dialogKey = '';
  @state() private dialogValue = '';
  @state() private dialogDescription = '';
  @state() private dialogSensitive = false;
  @state() private dialogSecret = false;
  @state() private dialogInjectionMode: InjectionMode = 'always';
  @state() private dialogLoading = false;
  @state() private dialogError: string | null = null;

  // Delete
  @state() private deletingKey: string | null = null;

  static override styles = css`
    :host {
      display: block;
    }

    .page-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      margin-bottom: 1.5rem;
      gap: 1rem;
    }

    .page-header-info h1 {
      font-size: 1.5rem;
      font-weight: 700;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.25rem 0;
    }

    .page-header-info p {
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
      padding: 3rem 2rem;
      background: var(--scion-surface, #ffffff);
      border: 1px dashed var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
    }

    .empty-state > sl-icon {
      font-size: 3rem;
      color: var(--scion-text-muted, #64748b);
      opacity: 0.5;
      margin-bottom: 0.75rem;
    }

    .empty-state h3 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.5rem 0;
    }

    .empty-state p {
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1.25rem 0;
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
    void this.loadEnvVars();
  }

  private async loadEnvVars(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const response = await fetch('/api/v1/env?scope=user', {
        credentials: 'include',
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      const data = (await response.json()) as { envVars?: EnvVar[] } | EnvVar[];
      this.envVars = Array.isArray(data) ? data : data.envVars || [];
    } catch (err) {
      console.error('Failed to load environment variables:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load environment variables';
    } finally {
      this.loading = false;
    }
  }

  private openCreateDialog(): void {
    this.dialogMode = 'create';
    this.dialogKey = '';
    this.dialogValue = '';
    this.dialogDescription = '';
    this.dialogSensitive = false;
    this.dialogSecret = false;
    this.dialogInjectionMode = 'always';
    this.dialogError = null;
    this.dialogOpen = true;
  }

  private openEditDialog(envVar: EnvVar): void {
    this.dialogMode = 'edit';
    this.dialogKey = envVar.key;
    this.dialogValue = envVar.sensitive || envVar.secret ? '' : envVar.value;
    this.dialogDescription = envVar.description || '';
    this.dialogSensitive = envVar.sensitive;
    this.dialogSecret = envVar.secret;
    this.dialogInjectionMode = envVar.injectionMode || 'always';
    this.dialogError = null;
    this.dialogOpen = true;
  }

  private closeDialog(): void {
    this.dialogOpen = false;
  }

  private async handleSave(e: Event): Promise<void> {
    e.preventDefault();

    const key = this.dialogKey.trim();
    if (!key) {
      this.dialogError = 'Key is required';
      return;
    }

    if (this.dialogMode === 'create' && !this.dialogValue) {
      this.dialogError = 'Value is required';
      return;
    }

    this.dialogLoading = true;
    this.dialogError = null;

    try {
      const body: Record<string, unknown> = {
        value: this.dialogValue,
        scope: 'user',
        description: this.dialogDescription || undefined,
        sensitive: this.dialogSensitive,
        secret: this.dialogSecret,
        injectionMode: this.dialogInjectionMode,
      };

      const response = await fetch(`/api/v1/env/${encodeURIComponent(key)}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      this.closeDialog();
      await this.loadEnvVars();
    } catch (err) {
      console.error('Failed to save environment variable:', err);
      this.dialogError = err instanceof Error ? err.message : 'Failed to save';
    } finally {
      this.dialogLoading = false;
    }
  }

  private async handleDelete(envVar: EnvVar): Promise<void> {
    if (!confirm(`Delete environment variable "${envVar.key}"? This cannot be undone.`)) {
      return;
    }

    this.deletingKey = envVar.key;

    try {
      const response = await fetch(`/api/v1/env/${encodeURIComponent(envVar.key)}?scope=user`, {
        method: 'DELETE',
        credentials: 'include',
      });

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete (HTTP ${response.status})`);
      }

      await this.loadEnvVars();
    } catch (err) {
      console.error('Failed to delete environment variable:', err);
      alert(err instanceof Error ? err.message : 'Failed to delete');
    } finally {
      this.deletingKey = null;
    }
  }

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

  override render() {
    if (this.loading) {
      return this.renderLoading();
    }

    if (this.error) {
      return this.renderError();
    }

    return html`
      <div class="page-header">
        <div class="page-header-info">
          <h1>Environment Variables</h1>
          <p>Manage environment variables injected into your agents at runtime.</p>
        </div>
        <sl-button variant="primary" @click=${this.openCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Variable
        </sl-button>
      </div>

      ${this.envVars.length === 0 ? this.renderEmpty() : this.renderTable()} ${this.renderDialog()}
    `;
  }

  private renderTable() {
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
            ${this.envVars.map((envVar) => this.renderRow(envVar))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderRow(envVar: EnvVar) {
    const isDeleting = this.deletingKey === envVar.key;
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
            @click=${() => this.openEditDialog(envVar)}
          ></sl-icon-button>
          <sl-icon-button
            name="trash"
            label="Delete"
            ?disabled=${isDeleting}
            @click=${() => this.handleDelete(envVar)}
          ></sl-icon-button>
        </td>
      </tr>
    `;
  }

  private renderEmpty() {
    return html`
      <div class="empty-state">
        <sl-icon name="terminal"></sl-icon>
        <h3>No Environment Variables</h3>
        <p>Add environment variables that will be injected into your agents.</p>
        <sl-button variant="primary" size="small" @click=${this.openCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Variable
        </sl-button>
      </div>
    `;
  }

  private renderDialog() {
    const title =
      this.dialogMode === 'create' ? 'Add Environment Variable' : 'Edit Environment Variable';
    const isCreate = this.dialogMode === 'create';

    return html`
      <sl-dialog label=${title} ?open=${this.dialogOpen} @sl-request-close=${this.closeDialog}>
        <form class="dialog-form" @submit=${this.handleSave}>
          <sl-input
            label="Key"
            placeholder="e.g. API_TOKEN"
            value=${this.dialogKey}
            ?disabled=${!isCreate}
            @sl-input=${(e: Event) => {
              this.dialogKey = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="Value"
            placeholder=${this.dialogMode === 'edit' && (this.dialogSensitive || this.dialogSecret)
              ? 'Enter new value to update'
              : 'Variable value'}
            value=${this.dialogValue}
            type=${this.dialogSecret || this.dialogSensitive ? 'password' : 'text'}
            @sl-input=${(e: Event) => {
              this.dialogValue = (e.target as HTMLInputElement).value;
            }}
            ?required=${isCreate}
          ></sl-input>

          <sl-textarea
            label="Description"
            placeholder="Optional description"
            value=${this.dialogDescription}
            rows="2"
            resize="none"
            @sl-input=${(e: Event) => {
              this.dialogDescription = (e.target as HTMLTextAreaElement).value;
            }}
          ></sl-textarea>

          <div class="radio-field">
            <span class="radio-field-label">Inject</span>
            <sl-radio-group
              value=${this.dialogInjectionMode}
              @sl-change=${(e: Event) => {
                this.dialogInjectionMode = (e.target as HTMLInputElement).value as InjectionMode;
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
                .checked=${this.dialogSensitive}
                @change=${(e: Event) => {
                  this.dialogSensitive = (e.target as HTMLInputElement).checked;
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
                .checked=${this.dialogSecret}
                @change=${(e: Event) => {
                  this.dialogSecret = (e.target as HTMLInputElement).checked;
                  if (this.dialogSecret) {
                    this.dialogSensitive = true;
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

          ${this.dialogError ? html`<div class="dialog-error">${this.dialogError}</div>` : nothing}
        </form>

        <sl-button
          slot="footer"
          variant="default"
          @click=${this.closeDialog}
          ?disabled=${this.dialogLoading}
        >
          Cancel
        </sl-button>
        <sl-button
          slot="footer"
          variant="primary"
          ?loading=${this.dialogLoading}
          ?disabled=${this.dialogLoading}
          @click=${this.handleSave}
        >
          ${isCreate ? 'Create' : 'Update'}
        </sl-button>
      </sl-dialog>
    `;
  }

  private renderLoading() {
    return html`
      <div class="loading-state">
        <sl-spinner></sl-spinner>
        <p>Loading environment variables...</p>
      </div>
    `;
  }

  private renderError() {
    return html`
      <div class="error-state">
        <sl-icon name="exclamation-triangle"></sl-icon>
        <h2>Failed to Load</h2>
        <p>There was a problem loading your environment variables.</p>
        <div class="error-details">${this.error}</div>
        <sl-button variant="primary" @click=${() => this.loadEnvVars()}>
          <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
          Retry
        </sl-button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-page-profile-env-vars': ScionPageProfileEnvVars;
  }
}
