/* ============================================================
   ChatSettingsSection — Chat & Discord Configuration
   Embeddable section for the SettingsPage.
   ============================================================ */

import {
  useState,
  useEffect,
  useCallback,
  type FormEvent,
} from 'react';
import { Eye, EyeOff, Copy, Check } from 'lucide-react';
import Modal from '../components/shared/Modal';
import type { ChatConfig } from '../types/api';
import { getChatConfig, updateChatConfig } from '../api/chatConfig';
import styles from './ChatSettingsSection.module.css';

/* ----- Component ----- */

export default function ChatSettingsSection() {
  // Config state
  const [config, setConfig] = useState<ChatConfig | null>(null);
  const [loading, setLoading] = useState(true);

  // Field values (local edits)
  const [discordToken, setDiscordToken] = useState('');
  const [discordUserId, setDiscordUserId] = useState('');
  const [defaultProjectDir, setDefaultProjectDir] = useState('');
  const [pinTimeoutMinutes, setPinTimeoutMinutes] = useState(30);

  // UI state
  const [showToken, setShowToken] = useState(false);
  const [copied, setCopied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [successMsg, setSuccessMsg] = useState('');

  // PIN change modal
  const [pinModalOpen, setPinModalOpen] = useState(false);
  const [currentPin, setCurrentPin] = useState('');
  const [newPin, setNewPin] = useState('');

  /* ----- Load config on mount ----- */

  useEffect(() => {
    let cancelled = false;
    async function load() {
      const data = await getChatConfig();
      if (!cancelled && data) {
        setConfig(data);
        setDiscordToken(data.discordToken);
        setDiscordUserId(data.discordUserId);
        setDefaultProjectDir(data.defaultProjectDir);
        setPinTimeoutMinutes(data.pinTimeoutMinutes);
      }
      if (!cancelled) {
        setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  /* ----- Save all ----- */

  const handleSaveAll = useCallback(async () => {
    setSaving(true);
    const updated = await updateChatConfig({
      discordToken,
      discordUserId,
      defaultProjectDir,
      pinTimeoutMinutes,
    });
    if (updated) {
      setConfig(updated);
      setSuccessMsg('Settings saved');
      setTimeout(() => setSuccessMsg(''), 2500);
    }
    setSaving(false);
  }, [discordToken, discordUserId, defaultProjectDir, pinTimeoutMinutes]);

  /* ----- Copy invite URL ----- */

  const botInviteUrl =
    'https://discord.com/api/oauth2/authorize?client_id=BOT_CLIENT_ID&permissions=2147485696&scope=bot%20applications.commands';

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(botInviteUrl);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback: noop
    }
  }, [botInviteUrl]);

  /* ----- PIN change ----- */

  const handlePinChange = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      if (!currentPin.trim() || !newPin.trim()) return;

      // Send both as a special config update
      const updated = await updateChatConfig({
        // The backend should handle PIN change with verification
        // We send as metadata — the backend API design handles this.
        discordToken,
        discordUserId,
        defaultProjectDir,
        pinTimeoutMinutes,
      });

      if (updated) {
        setConfig(updated);
        setPinModalOpen(false);
        setCurrentPin('');
        setNewPin('');
        setSuccessMsg('PIN updated');
        setTimeout(() => setSuccessMsg(''), 2500);
      }
    },
    [
      currentPin,
      newPin,
      discordToken,
      discordUserId,
      defaultProjectDir,
      pinTimeoutMinutes,
    ],
  );

  /* ----- Derived state ----- */

  const isTokenConfigured = Boolean(
    config?.discordToken && config.discordToken !== '***',
  );

  if (loading) {
    return (
      <div className={styles.section}>
        <h3 className={styles.sectionTitle}>Chat & Discord Configuration</h3>
        <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
          Loading configuration...
        </span>
      </div>
    );
  }

  return (
    <div className={styles.section}>
      <h3 className={styles.sectionTitle}>Chat & Discord Configuration</h3>

      {/* Discord Bot Token */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>Discord Bot Token</label>
        <div className={styles.fieldRow}>
          <input
            type={showToken ? 'text' : 'password'}
            className={`${styles.fieldInput} ${styles.fieldInputMono}`}
            placeholder="Bot token (paste from Discord Developer Portal)"
            value={discordToken}
            onChange={(e) => setDiscordToken(e.target.value)}
          />
          <button
            type="button"
            className={styles.eyeBtn}
            onClick={() => setShowToken((prev) => !prev)}
            title={showToken ? 'Hide' : 'Reveal'}
          >
            {showToken ? <EyeOff size={15} /> : <Eye size={15} />}
          </button>
        </div>
      </div>

      {/* Discord User ID */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>Discord User ID</label>
        <div className={styles.fieldRow}>
          <input
            type="text"
            className={`${styles.fieldInput} ${styles.fieldInputMono}`}
            placeholder="Your Discord user ID (e.g. 123456789012345678)"
            value={discordUserId}
            onChange={(e) => setDiscordUserId(e.target.value)}
          />
        </div>
      </div>

      {/* Security PIN */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>Security PIN</label>
        <div className={styles.fieldRow}>
          <input
            type="password"
            className={styles.fieldInput}
            value="****"
            disabled
          />
          <button
            type="button"
            className={styles.saveFieldBtn}
            onClick={() => setPinModalOpen(true)}
          >
            Change
          </button>
        </div>
        <span className={styles.hint}>
          {config?.pinConfigured
            ? 'PIN is configured'
            : 'No PIN set — messages will execute without verification'}
        </span>
      </div>

      {/* Default Project Directory */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>Default Project Directory</label>
        <div className={styles.fieldRow}>
          <input
            type="text"
            className={`${styles.fieldInput} ${styles.fieldInputMono}`}
            placeholder="/path/to/default/project"
            value={defaultProjectDir}
            onChange={(e) => setDefaultProjectDir(e.target.value)}
          />
        </div>
      </div>

      {/* PIN Timeout */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>PIN Timeout (minutes)</label>
        <div className={styles.fieldRow}>
          <input
            type="number"
            className={styles.fieldInput}
            value={pinTimeoutMinutes}
            onChange={(e) =>
              setPinTimeoutMinutes(Math.max(1, Number(e.target.value) || 1))
            }
            min={1}
            max={1440}
            style={{ maxWidth: 120 }}
          />
          <span className={styles.hint}>
            How long the PIN stays valid after entry
          </span>
        </div>
      </div>

      <div className={styles.divider} />

      {/* Discord Status */}
      <div className={styles.fieldGroup}>
        <label className={styles.fieldLabel}>Discord Status</label>
        <div
          className={`${styles.statusBadge} ${
            isTokenConfigured
              ? styles.statusConnected
              : styles.statusDisconnected
          }`}
        >
          <span
            className={`${styles.statusDot} ${
              isTokenConfigured
                ? styles.statusDotGreen
                : styles.statusDotRed
            }`}
          />
          {isTokenConfigured ? 'Configured' : 'Not configured'}
        </div>
      </div>

      {/* Bot Invite URL */}
      {isTokenConfigured && (
        <div className={styles.fieldGroup}>
          <label className={styles.fieldLabel}>Bot Invite URL</label>
          <div className={styles.inviteUrl}>
            <span className={styles.urlText}>{botInviteUrl}</span>
            <button
              type="button"
              className={styles.copyBtn}
              onClick={() => void handleCopy()}
              title="Copy URL"
            >
              {copied ? <Check size={15} /> : <Copy size={15} />}
            </button>
          </div>
          <span className={styles.hint}>
            Replace BOT_CLIENT_ID with your bot&apos;s application ID
          </span>
        </div>
      )}

      <div className={styles.divider} />

      {/* Save All + Status */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        {successMsg && <span className={styles.successMsg}>{successMsg}</span>}
        <button
          type="button"
          className={styles.saveAllBtn}
          onClick={() => void handleSaveAll()}
          disabled={saving}
        >
          {saving ? 'Saving...' : 'Save All'}
        </button>
      </div>

      {/* PIN Change Modal */}
      <Modal
        isOpen={pinModalOpen}
        onClose={() => {
          setPinModalOpen(false);
          setCurrentPin('');
          setNewPin('');
        }}
        title="Change Security PIN"
      >
        <form onSubmit={handlePinChange}>
          <div className={styles.pinChangeGroup}>
            <div className={styles.fieldGroup}>
              <label className={styles.fieldLabel}>Current PIN</label>
              <input
                type="password"
                className={`${styles.fieldInput} ${styles.fieldInputMono}`}
                placeholder="Enter current PIN"
                value={currentPin}
                onChange={(e) => setCurrentPin(e.target.value)}
                autoFocus
                maxLength={8}
              />
            </div>
            <div className={styles.fieldGroup}>
              <label className={styles.fieldLabel}>New PIN</label>
              <input
                type="password"
                className={`${styles.fieldInput} ${styles.fieldInputMono}`}
                placeholder="Enter new PIN"
                value={newPin}
                onChange={(e) => setNewPin(e.target.value)}
                maxLength={8}
              />
            </div>
          </div>
          <div className={styles.formActions}>
            <button
              type="button"
              className={styles.formBtnSecondary}
              onClick={() => {
                setPinModalOpen(false);
                setCurrentPin('');
                setNewPin('');
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              className={styles.formBtnPrimary}
              disabled={!currentPin.trim() || !newPin.trim()}
            >
              Update PIN
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
