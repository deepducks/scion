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
 * Profile Secrets Management page
 *
 * Manages user-scoped secrets. Secret values are write-only and never
 * returned from the API. Only metadata (key, type, version, etc.) is displayed.
 */

import { LitElement, html, css, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import type { Secret, SecretType, InjectionMode } from '../../shared/types.js';

@customElement('scion-page-profile-secrets')
export class ScionPageProfileSecrets extends LitElement {
  @state() private loading = true;
  @state() private secrets: Secret[] = [];
  @state() private error: string | null = null;

  // Create/Update dialog
  @state() private dialogOpen = false;
  @state() private dialogMode: 'create' | 'update' = 'create';
  @state() private dialogKey = '';
  @state() private dialogValue = '';
  @state() private dialogDescription = '';
  @state() private dialogType: SecretType = 'environment';
  @state() private dialogTarget = '';
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

    .description-cell {
      max-width: 200px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: var(--scion-text-muted, #64748b);
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

    .badge {
      display: inline-flex;
      align-items: center;
      gap: 0.25rem;
      padding: 0.125rem 0.5rem;
      border-radius: 9999px;
      font-size: 0.6875rem;
      font-weight: 500;
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
    void this.loadSecrets();
  }

  private async loadSecrets(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const response = await fetch('/api/v1/secrets?scope=user', {
        credentials: 'include',
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      const data = (await response.json()) as { secrets?: Secret[] } | Secret[];
      this.secrets = Array.isArray(data) ? data : data.secrets || [];
    } catch (err) {
      console.error('Failed to load secrets:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load secrets';
    } finally {
      this.loading = false;
    }
  }

  private openCreateDialog(): void {
    this.dialogMode = 'create';
    this.dialogKey = '';
    this.dialogValue = '';
    this.dialogDescription = '';
    this.dialogType = 'environment';
    this.dialogTarget = '';
    this.dialogInjectionMode = 'always';
    this.dialogError = null;
    this.dialogOpen = true;
  }

  private openUpdateDialog(secret: Secret): void {
    this.dialogMode = 'update';
    this.dialogKey = secret.key;
    this.dialogValue = '';
    this.dialogDescription = secret.description || '';
    this.dialogType = secret.type;
    this.dialogTarget = secret.target || '';
    this.dialogInjectionMode = secret.injectionMode || 'always';
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

    if (!this.dialogValue) {
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
        type: this.dialogType,
        target: this.dialogTarget || undefined,
        injectionMode: this.dialogInjectionMode,
      };

      const response = await fetch(`/api/v1/secrets/${encodeURIComponent(key)}`, {
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
      await this.loadSecrets();
    } catch (err) {
      console.error('Failed to save secret:', err);
      this.dialogError = err instanceof Error ? err.message : 'Failed to save';
    } finally {
      this.dialogLoading = false;
    }
  }

  private async handleDelete(secret: Secret): Promise<void> {
    if (!confirm(`Delete secret "${secret.key}"? This cannot be undone.`)) {
      return;
    }

    this.deletingKey = secret.key;

    try {
      const response = await fetch(`/api/v1/secrets/${encodeURIComponent(secret.key)}?scope=user`, {
        method: 'DELETE',
        credentials: 'include',
      });

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete (HTTP ${response.status})`);
      }

      await this.loadSecrets();
    } catch (err) {
      console.error('Failed to delete secret:', err);
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
          <h1>Secrets</h1>
          <p>
            Manage encrypted secrets for your agents. Values are write-only and cannot be retrieved
            after saving.
          </p>
        </div>
        <sl-button variant="primary" @click=${this.openCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Secret
        </sl-button>
      </div>

      ${this.secrets.length === 0 ? this.renderEmpty() : this.renderTable()} ${this.renderDialog()}
    `;
  }

  private renderTable() {
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
            ${this.secrets.map((secret) => this.renderRow(secret))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderRow(secret: Secret) {
    const isDeleting = this.deletingKey === secret.key;

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
            @click=${() => this.openUpdateDialog(secret)}
          ></sl-icon-button>
          <sl-icon-button
            name="trash"
            label="Delete"
            ?disabled=${isDeleting}
            @click=${() => this.handleDelete(secret)}
          ></sl-icon-button>
        </td>
      </tr>
    `;
  }

  private renderEmpty() {
    return html`
      <div class="empty-state">
        <sl-icon name="shield-lock"></sl-icon>
        <h3>No Secrets</h3>
        <p>Add encrypted secrets that will be securely injected into your agents.</p>
        <sl-button variant="primary" size="small" @click=${this.openCreateDialog}>
          <sl-icon slot="prefix" name="plus-lg"></sl-icon>
          Add Secret
        </sl-button>
      </div>
    `;
  }

  private renderDialog() {
    const isCreate = this.dialogMode === 'create';
    const title = isCreate ? 'Add Secret' : 'Update Secret';

    return html`
      <sl-dialog label=${title} ?open=${this.dialogOpen} @sl-request-close=${this.closeDialog}>
        <form class="dialog-form" @submit=${this.handleSave}>
          <sl-input
            label=${this.dialogType === 'file' ? 'Name' : 'Key'}
            placeholder=${this.dialogType === 'file' ? 'e.g. ssh_deploy_key' : 'e.g. GITHUB_TOKEN'}
            value=${this.dialogKey}
            ?disabled=${!isCreate}
            @sl-input=${(e: Event) => {
              this.dialogKey = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="Value"
            placeholder="Secret value"
            value=${this.dialogValue}
            type="password"
            @sl-input=${(e: Event) => {
              this.dialogValue = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <div class="dialog-hint">
            <sl-icon name="info-circle"></sl-icon>
            Secret values are encrypted and can never be retrieved after saving.
          </div>

          <sl-select
            label="Type"
            value=${this.dialogType}
            @sl-change=${(e: Event) => {
              this.dialogType = (e.target as HTMLSelectElement).value as SecretType;
            }}
          >
            <sl-option value="environment">Environment Variable</sl-option>
            <sl-option value="variable">Runtime Variable</sl-option>
            <sl-option value="file">File</sl-option>
          </sl-select>

          ${this.dialogType === 'file'
            ? html`
                <sl-input
                  label="Target Path"
                  placeholder="e.g. /home/agent/.ssh/id_rsa"
                  value=${this.dialogTarget}
                  @sl-input=${(e: Event) => {
                    this.dialogTarget = (e.target as HTMLInputElement).value;
                  }}
                ></sl-input>
              `
            : nothing}

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
        <p>Loading secrets...</p>
      </div>
    `;
  }

  private renderError() {
    return html`
      <div class="error-state">
        <sl-icon name="exclamation-triangle"></sl-icon>
        <h2>Failed to Load</h2>
        <p>There was a problem loading your secrets.</p>
        <div class="error-details">${this.error}</div>
        <sl-button variant="primary" @click=${() => this.loadSecrets()}>
          <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
          Retry
        </sl-button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-page-profile-secrets': ScionPageProfileSecrets;
  }
}
