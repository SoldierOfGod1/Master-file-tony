/* ClickUp settings form — lets the user rotate the API token, workspace,
   and list at runtime. Writing a new list id triggers the backend to push
   the 10-status pipeline into that new list (async, on the backend side). */

import { type FormEvent, useCallback, useEffect, useState } from 'react';
import { CheckCircle2, AlertCircle, Save, ExternalLink } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { getSettings, updateSettings, type AppSettings } from '../api/settings';
import styles from './ClickUpSettingsSection.module.css';

/** True if the value the backend returned for api_token is the masked
 *  placeholder. We leave it alone on save so re-submitting the form doesn't
 *  overwrite the real token with the masked string. */
function isMasked(tok: string): boolean {
  return tok.startsWith('••');
}

export default function ClickUpSettingsSection() {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<'idle' | 'ok' | 'error'>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  const [token, setToken] = useState('');
  const [workspaceId, setWorkspaceId] = useState('');
  const [listId, setListId] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    const s = await getSettings();
    if (s) {
      setSettings(s);
      setToken(s['clickup.api_token'] ?? '');
      setWorkspaceId(s['clickup.workspace_id'] ?? '');
      setListId(s['clickup.list_id'] ?? '');
    }
    setLoading(false);
  }, []);

  useEffect(() => { void load(); }, [load]);

  const handleSubmit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatus('idle');
    setErrorMsg('');
    // Skip the token field if the user left the masked value untouched.
    const patch: Record<string, string> = {
      'clickup.workspace_id': workspaceId.trim(),
      'clickup.list_id': listId.trim(),
    };
    if (!isMasked(token)) {
      patch['clickup.api_token'] = token.trim();
    }
    const result = await updateSettings(patch);
    setSaving(false);
    if (result) {
      setSettings(result);
      setStatus('ok');
      setToken(result['clickup.api_token'] ?? '');
    } else {
      setStatus('error');
      setErrorMsg('Failed to save — check the backend log.');
    }
  }, [token, workspaceId, listId]);

  const configured = settings?.['clickup.configured'] === true;
  const ledColor = loading ? '#7cc6ff' : configured ? '#6ff2a0' : '#ffaa00';

  return (
    <HudPanel
      title="ClickUp Integration"
      accent={configured ? '#6ff2a0' : '#ffaa00'}
      leading={<HudStatusLed color={ledColor} animate={!loading} />}
      meta={
        <HudChip color={configured ? '#6ff2a0' : '#ffaa00'}>
          {loading ? 'loading' : configured ? 'Connected' : 'Not configured'}
        </HudChip>
      }
    >
      <form onSubmit={handleSubmit} className={styles.form}>
        <p className={styles.helpText}>
          These credentials drive the Projects ⇄ ClickUp sync. Changing the
          <strong> list ID </strong> triggers a backend job that installs the
          10 canonical project statuses (<code>To Do</code>, <code>In Progress</code>,
          <code>SIT</code>, <code>QA</code>, <code>PPD</code>, <code>QA Fail</code>,
          <code>Blocker</code>, <code>SIT Pass</code>, <code>PPD Pass</code>,
          <code>Completed</code>) onto that list.
        </p>

        <div className={styles.field}>
          <label className={styles.label}>
            API Token
            <a
              href="https://app.clickup.com/settings/apps"
              target="_blank"
              rel="noreferrer"
              className={styles.inlineLink}
            >
              Generate <ExternalLink size={10} />
            </a>
          </label>
          <input
            type="password"
            className={styles.input}
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="pk_..."
            autoComplete="off"
          />
          <div className={styles.hint}>
            {isMasked(token)
              ? 'Token is set — edit to replace, or leave masked to keep.'
              : 'Paste a personal token (pk_…). Never commits to git.'}
          </div>
        </div>

        <div className={styles.field}>
          <label className={styles.label}>Workspace ID</label>
          <input
            type="text"
            className={styles.input}
            value={workspaceId}
            onChange={(e) => setWorkspaceId(e.target.value)}
            placeholder="e.g. 90121466494"
            autoComplete="off"
          />
          <div className={styles.hint}>
            From <code>app.clickup.com/&lt;workspace-id&gt;/...</code>
          </div>
        </div>

        <div className={styles.field}>
          <label className={styles.label}>List ID</label>
          <input
            type="text"
            className={styles.input}
            value={listId}
            onChange={(e) => setListId(e.target.value)}
            placeholder="e.g. 901216526868"
            autoComplete="off"
          />
          <div className={styles.hint}>
            Right-click a list in ClickUp → Copy link → the digits after <code>l/li/</code>.
          </div>
        </div>

        <div className={styles.actions}>
          <button
            type="submit"
            className={styles.saveBtn}
            disabled={saving || loading}
          >
            <Save size={13} />
            {saving ? 'Saving…' : 'Save'}
          </button>
          {status === 'ok' && (
            <span className={styles.okText}>
              <CheckCircle2 size={12} /> Saved — sync engine will pick it up on the next tick.
            </span>
          )}
          {status === 'error' && (
            <span className={styles.errorText}>
              <AlertCircle size={12} /> {errorMsg}
            </span>
          )}
        </div>
      </form>
    </HudPanel>
  );
}
