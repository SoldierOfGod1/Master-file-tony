/* Athena settings — Customer 360 CDR usage panel. Values are read
   server-side at startup: app_settings wins over env vars. Saving
   here requires a backend restart to take effect (AWS SDK builds
   its client once per process). The UI flags that behaviour. */

import { type FormEvent, useCallback, useEffect, useState } from 'react';
import { Save, Cloud } from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { getSettings, updateSettings, type AppSettings } from '../api/settings';
import styles from './ClickUpSettingsSection.module.css';

function isMasked(v: string): boolean {
  return v.startsWith('••');
}

export default function AthenaSettingsSection() {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<'idle' | 'ok' | 'error'>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  const [enabled, setEnabled] = useState(true);
  const [region, setRegion] = useState('eu-west-1');
  const [database, setDatabase] = useState('usage');
  const [workgroup, setWorkgroup] = useState('');
  const [outputS3, setOutputS3] = useState('');
  const [accessKey, setAccessKey] = useState('');
  const [secretKey, setSecretKey] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    const s = await getSettings();
    if (s) {
      setSettings(s);
      setEnabled((s['athena.enabled'] ?? 'true') !== 'false');
      setRegion(s['athena.region'] ?? 'eu-west-1');
      setDatabase(s['athena.database'] ?? 'usage');
      setWorkgroup(s['athena.workgroup'] ?? '');
      setOutputS3(s['athena.output_s3'] ?? '');
      setAccessKey(s['athena.aws_access_key_id'] ?? '');
      setSecretKey(s['athena.aws_secret_access_key'] ?? '');
    }
    setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  const handleSubmit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatus('idle');
    setErrorMsg('');
    const patch: Record<string, string> = {
      'athena.enabled': enabled ? 'true' : 'false',
      'athena.region': region.trim(),
      'athena.database': database.trim(),
      'athena.workgroup': workgroup.trim(),
      'athena.output_s3': outputS3.trim(),
      'athena.aws_access_key_id': accessKey.trim(),
    };
    // Skip the secret if the user left the masked value untouched.
    if (!isMasked(secretKey)) {
      patch['athena.aws_secret_access_key'] = secretKey.trim();
    }
    const result = await updateSettings(patch);
    setSaving(false);
    if (result) {
      setSettings(result);
      setStatus('ok');
      setSecretKey(result['athena.aws_secret_access_key'] ?? '');
    } else {
      setStatus('error');
      setErrorMsg('Failed to save — check the backend log.');
    }
  }, [enabled, region, database, workgroup, outputS3, accessKey, secretKey]);

  const configured = settings?.['athena.configured'] === true;
  const ledColor = loading ? '#7cc6ff' : configured ? '#6ff2a0' : '#ffaa00';

  return (
    <HudPanel
      title="Athena CDR Usage"
      accent={configured ? '#6ff2a0' : '#ffaa00'}
      icon={<Cloud size={12} />}
      leading={<HudStatusLed color={ledColor} animate={!loading} />}
      meta={
        <HudChip color={configured ? '#6ff2a0' : '#ffaa00'}>
          {loading ? 'loading' : configured ? 'Configured' : 'Not configured'}
        </HudChip>
      }
    >
      <form onSubmit={handleSubmit} className={styles.form}>
        <p style={{ fontSize: 11, opacity: 0.8, margin: '0 0 8px 0' }}>
          Powers the <b>Customer 360 · Data Usage</b> panel. Region + S3
          output are required. AWS keys are optional — the SDK falls
          back to <code>~/.aws/credentials</code> or an IAM role if you
          leave them empty. <b>Saving here requires a backend restart</b>
          for the AWS client to pick up new values.
        </p>

        <label className={styles.row}>
          <span className={styles.label}>Enable Athena queries</span>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            style={{ transform: 'scale(1.2)', marginLeft: 8 }}
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>AWS Region</span>
          <input
            className={styles.input}
            value={region}
            onChange={(e) => setRegion(e.target.value)}
            placeholder="eu-west-1"
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>Database (Glue)</span>
          <input
            className={styles.input}
            value={database}
            onChange={(e) => setDatabase(e.target.value)}
            placeholder="usage"
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>Workgroup (optional)</span>
          <input
            className={styles.input}
            value={workgroup}
            onChange={(e) => setWorkgroup(e.target.value)}
            placeholder="primary"
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>S3 Output Location *</span>
          <input
            className={styles.input}
            value={outputS3}
            onChange={(e) => setOutputS3(e.target.value)}
            placeholder="s3://rain-athena-results/cc/"
            required
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>AWS Access Key ID</span>
          <input
            className={styles.input}
            value={accessKey}
            onChange={(e) => setAccessKey(e.target.value)}
            placeholder="(leave empty to use AWS default chain)"
          />
        </label>

        <label className={styles.row}>
          <span className={styles.label}>AWS Secret Access Key</span>
          <input
            type="password"
            className={styles.input}
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder="(leave empty to use AWS default chain)"
          />
        </label>

        <div className={styles.actions}>
          <button type="submit" className={styles.saveBtn} disabled={saving}>
            <Save size={12} /> {saving ? 'saving…' : 'Save'}
          </button>
          {status === 'ok' && (
            <span style={{ color: '#6ff2a0', fontSize: 11 }}>
              Saved — restart backend for the AWS client to reload.
            </span>
          )}
          {status === 'error' && (
            <span style={{ color: '#ff7b7b', fontSize: 11 }}>{errorMsg}</span>
          )}
        </div>
      </form>
    </HudPanel>
  );
}
