/* Multi-connection DB settings — powers the Customer 360 tab and any
   future DB-backed feature. Each row is one cluster; user can add, edit,
   test, set primary, or delete. Passwords are masked on read. */

import { type FormEvent, useCallback, useEffect, useState } from 'react';
import {
  Database,
  Save,
  TestTube,
  CheckCircle2,
  AlertCircle,
  Star,
  Trash2,
  Plus,
  X,
} from 'lucide-react';
import HudPanel from '../components/shared/HudPanel';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import Modal from '../components/shared/Modal';
import {
  type DBConnection,
  type DBDriver,
  deleteConnection,
  listConnections,
  setPrimary,
  testConnection,
  upsertConnection,
} from '../api/connections';
import styles from './DatabaseConnectionsSection.module.css';

const DRIVERS: { value: DBDriver; label: string; note: string }[] = [
  { value: 'postgres',   label: 'Postgres',   note: 'Used by Customer 360 for identity / payment / contact queries.' },
  { value: 'clickhouse', label: 'ClickHouse', note: 'Reserved slot — queries not wired yet. Still useful to pre-seed creds.' },
];

/* Connection presets — one-click prefill for new connections.
   Saves the user typing host/port/database for the canonical
   rain clusters. Custom = blank. Adding a new preset here is the
   right place for any new BSS-critical DB the platform monitor
   should treat as P1 (the id-prefix decides — see backend
   platforms/dbhealth.go CRITICAL_DB_PREFIXES). */
interface ConnectionPreset {
  readonly id: string;
  readonly label: string;
  readonly hint: string;
  readonly driver: DBDriver;
  readonly host: string;
  readonly port: string;
  readonly database: string;
  readonly sslMode: string;
}

const CONNECTION_PRESETS: readonly ConnectionPreset[] = [
  {
    id: 'gaussdb-prod',
    label: 'Huawei GaussDB DWS · PROD',
    hint: 'Wire-compatible with Postgres. Monitored at P1 priority alongside Axiom.',
    driver: 'postgres',
    host: '10.20.48.183',
    port: '8000',
    database: 'gaussdb',
    sslMode: 'disable',
  },
  {
    id: 'axiom-prod',
    label: 'Axiom · PROD',
    hint: 'rain BSS prod cluster. Read-only access; writes route through Snowflake middleware.',
    driver: 'postgres',
    host: 'axiom-prod-pg-cluster.rain.co.za',
    port: '5433',
    database: 'postgresdb',
    sslMode: 'require',
  },
  {
    id: 'axiom-sit-cluster',
    label: 'Axiom SIT · Cluster',
    hint: 'SIT mirror. Use this for the SIM cascade probe and any agent dev work.',
    driver: 'postgres',
    host: 'sit-pg-cluster.rain.co.za',
    port: '5433',
    database: 'postgresdb',
    sslMode: 'disable',
  },
  {
    id: 'axiom-sit-bss',
    label: 'Axiom BSS · SIT',
    hint: 'BSS-flavoured SIT instance.',
    driver: 'postgres',
    host: 'bss-psql-sit-01.rain.network',
    port: '5432',
    database: 'postgresdb',
    sslMode: 'disable',
  },
  {
    id: 'clickhouse-main',
    label: 'ClickHouse · HouseOfClicks',
    hint: 'rain analytics.',
    driver: 'clickhouse',
    host: 'houseofclicks.rain.co.za',
    port: '8123',
    database: 'default',
    sslMode: 'disable',
  },
];

function isMasked(v: string): boolean {
  return v.startsWith('••');
}

/* Inline connection card — a compact summary + expandable edit form. */
function ConnectionCard({
  conn, onChange, onDelete, busy,
}: {
  readonly conn: DBConnection;
  readonly onChange: () => void;
  readonly onDelete: (id: string) => void;
  readonly busy: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [label, setLabel] = useState(conn.label);
  const [host, setHost] = useState(conn.host);
  const [port, setPort] = useState(conn.port);
  const [database, setDatabase] = useState(conn.database);
  const [user, setUser] = useState(conn.user);
  const [password, setPassword] = useState(conn.password);
  const [sslMode, setSslMode] = useState(conn.ssl_mode);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [status, setStatus] = useState<'idle' | 'ok' | 'error'>('idle');
  const [errMsg, setErrMsg] = useState('');

  // Re-hydrate the local form state if the parent refreshes with new data
  // (e.g. after Save reloads the list).
  useEffect(() => {
    setLabel(conn.label);
    setHost(conn.host);
    setPort(conn.port);
    setDatabase(conn.database);
    setUser(conn.user);
    setPassword(conn.password);
    setSslMode(conn.ssl_mode);
  }, [conn]);

  const doSave = useCallback(async () => {
    setSaving(true);
    setStatus('idle');
    setErrMsg('');
    const patch: Partial<DBConnection> = {
      id: conn.id,
      label, host, port, database, user,
      driver: conn.driver,
      ssl_mode: sslMode,
    };
    if (!isMasked(password)) {
      patch.password = password;
    }
    const result = await upsertConnection(patch as DBConnection);
    setSaving(false);
    if (result) {
      setStatus('ok');
      onChange();
    } else {
      setStatus('error');
      setErrMsg('Save failed — check backend log');
    }
  }, [conn.id, conn.driver, label, host, port, database, user, password, sslMode, onChange]);

  const doTest = useCallback(async () => {
    // Save first so the backend pings with the latest values.
    await doSave();
    setTesting(true);
    setStatus('idle');
    setErrMsg('');
    const result = await testConnection(conn.id);
    setTesting(false);
    if (result.ok) setStatus('ok');
    else { setStatus('error'); setErrMsg(result.error); }
  }, [conn.id, doSave]);

  const doSetPrimary = useCallback(async () => {
    await setPrimary(conn.id);
    onChange();
  }, [conn.id, onChange]);

  const doDelete = useCallback(() => {
    if (!window.confirm(`Delete connection "${conn.label}"? This can't be undone.`)) return;
    onDelete(conn.id);
  }, [conn.id, conn.label, onDelete]);

  const ledColor = conn.is_primary ? '#00f0ff' : conn.filled ? '#6ff2a0' : '#ffaa00';

  return (
    <div className={`${styles.connCard} ${conn.is_primary ? styles.connCardPrimary : ''}`}>
      <button
        type="button"
        className={styles.connHead}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={styles.connLedWrap}>
          <HudStatusLed color={ledColor} animate={conn.is_primary} />
        </span>
        <div className={styles.connTitle}>
          <span className={styles.connLabel}>{conn.label}</span>
          <span className={styles.connHost}>
            <Database size={10} /> {conn.host || 'host not set'}:{conn.port}
          </span>
        </div>
        <div className={styles.connBadges}>
          <HudChip color={conn.driver === 'postgres' ? '#00f0ff' : '#ff7de0'}>
            {conn.driver}
          </HudChip>
          {conn.is_primary && (
            <HudChip color="#00f0ff">
              <Star size={9} style={{ marginRight: 3 }} />
              PRIMARY
            </HudChip>
          )}
          {!conn.filled && (
            <HudChip color="#ffaa00">needs pw</HudChip>
          )}
        </div>
      </button>

      {open && (
        <div className={styles.connBody}>
          <div className={styles.twoCol}>
            <div className={styles.field}>
              <label className={styles.label}>Label</label>
              <input
                type="text"
                className={styles.input}
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
            </div>
            <div className={styles.field}>
              <label className={styles.label}>Host</label>
              <input
                type="text"
                className={styles.input}
                value={host}
                onChange={(e) => setHost(e.target.value)}
              />
            </div>
          </div>

          <div className={styles.twoCol}>
            <div className={styles.field}>
              <label className={styles.label}>Port</label>
              <input
                type="text"
                className={styles.input}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                inputMode="numeric"
              />
            </div>
            <div className={styles.field}>
              <label className={styles.label}>Database</label>
              <input
                type="text"
                className={styles.input}
                value={database}
                onChange={(e) => setDatabase(e.target.value)}
              />
            </div>
          </div>

          <div className={styles.twoCol}>
            <div className={styles.field}>
              <label className={styles.label}>User</label>
              <input
                type="text"
                className={styles.input}
                value={user}
                onChange={(e) => setUser(e.target.value)}
              />
            </div>
            <div className={styles.field}>
              <label className={styles.label}>Password</label>
              <input
                type="password"
                className={styles.input}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="paste password"
              />
            </div>
          </div>

          <div className={styles.field}>
            <label className={styles.label}>SSL Mode</label>
            <select
              className={styles.input}
              value={sslMode}
              onChange={(e) => setSslMode(e.target.value)}
            >
              <option value="disable">disable</option>
              <option value="require">require</option>
              <option value="verify-full">verify-full</option>
            </select>
          </div>

          <div className={styles.actions}>
            <button
              type="button"
              className={styles.saveBtn}
              onClick={() => void doSave()}
              disabled={saving || busy}
            >
              <Save size={12} /> {saving ? 'Saving…' : 'Save'}
            </button>
            <button
              type="button"
              className={styles.testBtn}
              onClick={() => void doTest()}
              disabled={testing || busy}
            >
              <TestTube size={12} /> {testing ? 'Testing…' : 'Test'}
            </button>
            {!conn.is_primary && conn.filled && (
              <button
                type="button"
                className={styles.secondaryBtn}
                onClick={() => void doSetPrimary()}
              >
                <Star size={12} /> Make primary
              </button>
            )}
            <button
              type="button"
              className={styles.deleteBtn}
              onClick={doDelete}
            >
              <Trash2 size={12} /> Delete
            </button>

            {status === 'ok' && (
              <span className={styles.okText}>
                <CheckCircle2 size={11} /> Connected.
              </span>
            )}
            {status === 'error' && (
              <span className={styles.errorText}>
                <AlertCircle size={11} /> {errMsg}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

/* Modal body for adding a brand-new connection. */
function NewConnectionForm({ onCreated, onClose }: {
  readonly onCreated: () => void;
  readonly onClose: () => void;
}) {
  const [presetId, setPresetId] = useState<string>('');
  const [label, setLabel] = useState('');
  const [driver, setDriver] = useState<DBDriver>('postgres');
  const [host, setHost] = useState('');
  const [port, setPort] = useState('5432');
  const [database, setDatabase] = useState('postgresdb');
  const [user, setUser] = useState('');
  const [password, setPassword] = useState('');
  const [sslMode, setSslMode] = useState('disable');
  const [submitting, setSubmitting] = useState(false);

  const applyPreset = useCallback((id: string) => {
    setPresetId(id);
    if (id === '') return;
    const p = CONNECTION_PRESETS.find((x) => x.id === id);
    if (!p) return;
    setLabel(p.label);
    setDriver(p.driver);
    setHost(p.host);
    setPort(p.port);
    setDatabase(p.database);
    setSslMode(p.sslMode);
  }, []);

  const submit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    const result = await upsertConnection({
      label: label.trim(),
      driver,
      host: host.trim(),
      port: port.trim(),
      database: database.trim(),
      user: user.trim(),
      password,
      ssl_mode: sslMode,
    });
    setSubmitting(false);
    if (result) {
      onCreated();
      onClose();
    }
  }, [label, driver, host, port, database, user, password, sslMode, onCreated, onClose]);

  return (
    <form onSubmit={submit} className={styles.form}>
      <div className={styles.field}>
        <label className={styles.label}>Preset</label>
        <select
          className={styles.input}
          value={presetId}
          onChange={(e) => applyPreset(e.target.value)}
        >
          <option value="">Custom (start blank)</option>
          {CONNECTION_PRESETS.map((p) => (
            <option key={p.id} value={p.id}>{p.label}</option>
          ))}
        </select>
        <div className={styles.hint}>
          {presetId
            ? CONNECTION_PRESETS.find((p) => p.id === presetId)?.hint ?? ''
            : 'Pick a preset to prefill the canonical rain hosts, or leave blank for a custom connection.'}
        </div>
      </div>

      <div className={styles.field}>
        <label className={styles.label}>Label</label>
        <input
          type="text"
          className={styles.input}
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          placeholder="e.g. Network CPE"
          autoFocus
          required
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label}>Driver</label>
        <select
          className={styles.input}
          value={driver}
          onChange={(e) => setDriver(e.target.value as DBDriver)}
        >
          {DRIVERS.map((d) => <option key={d.value} value={d.value}>{d.label}</option>)}
        </select>
        <div className={styles.hint}>{DRIVERS.find((d) => d.value === driver)?.note}</div>
      </div>

      <div className={styles.twoCol}>
        <div className={styles.field}>
          <label className={styles.label}>Host</label>
          <input
            type="text"
            className={styles.input}
            value={host}
            onChange={(e) => setHost(e.target.value)}
            required
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label}>Port</label>
          <input
            type="text"
            className={styles.input}
            value={port}
            onChange={(e) => setPort(e.target.value)}
            inputMode="numeric"
          />
        </div>
      </div>

      <div className={styles.twoCol}>
        <div className={styles.field}>
          <label className={styles.label}>Database</label>
          <input
            type="text"
            className={styles.input}
            value={database}
            onChange={(e) => setDatabase(e.target.value)}
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label}>User</label>
          <input
            type="text"
            className={styles.input}
            value={user}
            onChange={(e) => setUser(e.target.value)}
          />
        </div>
      </div>

      <div className={styles.field}>
        <label className={styles.label}>Password</label>
        <input
          type="password"
          className={styles.input}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label}>SSL Mode</label>
        <select
          className={styles.input}
          value={sslMode}
          onChange={(e) => setSslMode(e.target.value)}
        >
          <option value="disable">disable</option>
          <option value="require">require</option>
          <option value="verify-full">verify-full</option>
        </select>
      </div>

      <div className={styles.modalActions}>
        <button type="button" className={styles.secondaryBtn} onClick={onClose}>
          <X size={12} /> Cancel
        </button>
        <button type="submit" className={styles.saveBtn} disabled={submitting}>
          <Save size={12} /> {submitting ? 'Creating…' : 'Create'}
        </button>
      </div>
    </form>
  );
}

export default function DatabaseConnectionsSection() {
  const [conns, setConns] = useState<DBConnection[]>([]);
  const [loading, setLoading] = useState(true);
  const [addOpen, setAddOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setConns(await listConnections());
    setLoading(false);
  }, []);

  useEffect(() => { void load(); }, [load]);

  const handleDelete = useCallback(async (id: string) => {
    setBusy(true);
    await deleteConnection(id);
    setBusy(false);
    void load();
  }, [load]);

  const configured = conns.some((c) => c.driver === 'postgres' && c.filled);
  const primary = conns.find((c) => c.is_primary);

  return (
    <HudPanel
      title="Database Connections"
      accent={configured ? '#6ff2a0' : '#ffaa00'}
      leading={<HudStatusLed color={configured ? '#6ff2a0' : '#ffaa00'} animate={!loading} />}
      meta={
        <HudChip color={configured ? '#6ff2a0' : '#ffaa00'}>
          {loading ? 'loading' : configured
            ? `${conns.filter((c) => c.filled).length} ready / ${conns.length}`
            : `${conns.length} connections · set a password`}
        </HudChip>
      }
    >
      <p className={styles.intro}>
        One row per database the dashboard can talk to. Clicking a row expands
        it for inline edit. The <strong>primary</strong> connection is what
        Customer 360 defaults to when no explicit connection is picked.
        {primary && (
          <> Currently primary: <strong>{primary.label}</strong>.</>
        )}
      </p>

      <div className={styles.connList}>
        {conns.map((c) => (
          <ConnectionCard
            key={c.id}
            conn={c}
            onChange={load}
            onDelete={handleDelete}
            busy={busy}
          />
        ))}
      </div>

      <div className={styles.addRow}>
        <button
          type="button"
          className={styles.addBtn}
          onClick={() => setAddOpen(true)}
        >
          <Plus size={13} /> Add connection
        </button>
      </div>

      <Modal
        isOpen={addOpen}
        onClose={() => setAddOpen(false)}
        title="Add database connection"
      >
        <NewConnectionForm
          onCreated={load}
          onClose={() => setAddOpen(false)}
        />
      </Modal>
    </HudPanel>
  );
}
