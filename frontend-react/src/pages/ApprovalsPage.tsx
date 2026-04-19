/* ============================================================
   ApprovalsPage — HUD grid of pending approval panels with
   Approve / Reject workflow + review modal. Same data model
   + mutation logic as before; only the chrome changed.
   ============================================================ */

import { useState, useMemo, useCallback } from 'react';
import {
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Clock,
  FileCode2,
  GitBranch,
  Server,
  Package,
  Inbox,
  User,
  type LucideIcon,
} from 'lucide-react';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import { updateApproval } from '../api/approvals';
import type { Approval } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './ApprovalsPage.module.css';

const FILTERS = ['All', 'Pending', 'Approved', 'Rejected'] as const;
type Filter = (typeof FILTERS)[number];

type ReviewAction = 'approved' | 'rejected';

interface ReviewModalState {
  readonly approval: Approval;
  readonly action: ReviewAction;
}

const TYPE_ICONS: Record<string, LucideIcon> = {
  deployment: Server,
  code: FileCode2,
  merge: GitBranch,
  release: Package,
};
const typeIcon = (t: string): LucideIcon => TYPE_ICONS[t.toLowerCase()] ?? FileCode2;

interface Pal { readonly color: string; readonly label: string; }
const STATUS: Record<string, Pal> = {
  pending:  { color: '#ffaa00', label: 'Pending' },
  approved: { color: '#6ff2a0', label: 'Approved' },
  rejected: { color: '#ff3355', label: 'Rejected' },
};
const PRIORITY: Record<string, Pal> = {
  critical: { color: '#ff3355', label: 'Critical' },
  high:     { color: '#ff7de0', label: 'High' },
  medium:   { color: '#ffaa00', label: 'Medium' },
  low:      { color: '#7cc6ff', label: 'Low' },
};
const palFor = (map: Record<string, Pal>, k: string, fallback: Pal): Pal =>
  map[k.toLowerCase()] ?? fallback;

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return iso;
  }
}

export default function ApprovalsPage() {
  const { state, refreshAll } = useCommandCentre();
  const [activeFilter, setActiveFilter] = useState<Filter>('All');
  const [reviewModal, setReviewModal] = useState<ReviewModalState | null>(null);
  const [comment, setComment] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const filtered = useMemo(() => {
    if (activeFilter === 'All') return state.approvals;
    return state.approvals.filter(
      (a) => a.status.toLowerCase() === activeFilter.toLowerCase(),
    );
  }, [state.approvals, activeFilter]);

  const counts = useMemo(() => {
    const m: Record<string, number> = { pending: 0, approved: 0, rejected: 0 };
    for (const a of state.approvals) {
      const k = a.status.toLowerCase();
      m[k] = (m[k] ?? 0) + 1;
    }
    return m;
  }, [state.approvals]);

  const pendingCount = counts.pending ?? 0;
  const total = state.approvals.length;
  const ratio = total === 0 ? 0 : (counts.approved ?? 0) / total;

  const openReview = useCallback((approval: Approval, action: ReviewAction) => {
    setReviewModal({ approval, action });
    setComment('');
  }, []);
  const closeReview = useCallback(() => {
    setReviewModal(null);
    setComment('');
  }, []);

  const handleSubmitReview = useCallback(async () => {
    if (!reviewModal) return;
    setSubmitting(true);
    try {
      await updateApproval(reviewModal.approval.id, {
        status: reviewModal.action,
        reviewComment: comment || undefined,
      });
      await refreshAll();
    } finally {
      setSubmitting(false);
      closeReview();
    }
  }, [reviewModal, comment, refreshAll, closeReview]);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Approvals · Review Queue"
        subtitle={`${total} total · ${pendingCount} pending`}
        gaugeValue={ratio}
        gaugeReadout={`${counts.approved ?? 0}/${total}`}
        gaugeLabel="APPROVED"
        gaugeColor="#6ff2a0"
        segments={Object.entries(STATUS).map(([k, p]) => ({
          label: p.label, value: counts[k] ?? 0, color: p.color,
        }))}
        extra={<div className={styles.shieldIcon}><ShieldCheck size={22} style={{ color: '#00f0ff' }} /></div>}
      />

      <div className={styles.filterRow}>
        {FILTERS.map((f) => (
          <button
            key={f}
            type="button"
            className={`${styles.filterBtn} ${activeFilter === f ? styles.filterBtnActive : ''}`}
            onClick={() => setActiveFilter(f)}
          >
            {f}
            {f !== 'All' && (
              <span className={styles.filterCount}>{counts[f.toLowerCase()] ?? 0}</span>
            )}
          </button>
        ))}
      </div>

      {filtered.length === 0 ? (
        <HudPanel title="Queue" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" animate={false} />}>
          <div className={styles.empty}>
            <Inbox size={36} className={styles.emptyIcon} />
            <span>No {activeFilter.toLowerCase()} approvals</span>
          </div>
        </HudPanel>
      ) : (
        <div className={hudStyles.gridWide}>
          {filtered.map((approval) => {
            const Icon = typeIcon(approval.type);
            const statusPal = palFor(STATUS, approval.status, STATUS.pending);
            const priorityPal = palFor(PRIORITY, approval.priority, PRIORITY.medium);
            const isPending = approval.status.toLowerCase() === 'pending';

            return (
              <HudPanel
                key={approval.id}
                title={approval.title}
                accent={statusPal.color}
                leading={<HudStatusLed color={statusPal.color} animate={isPending} />}
                meta={<Icon size={11} />}
                footer={
                  isPending ? (
                    <div className={styles.btnRow}>
                      <button
                        type="button"
                        className={styles.btnApprove}
                        onClick={() => openReview(approval, 'approved')}
                      >
                        <CheckCircle2 size={12} /> Approve
                      </button>
                      <button
                        type="button"
                        className={styles.btnReject}
                        onClick={() => openReview(approval, 'rejected')}
                      >
                        <XCircle size={12} /> Reject
                      </button>
                    </div>
                  ) : (
                    <div className={styles.resolvedFooter}>
                      <HudChip color={statusPal.color}>
                        {approval.status === 'approved' ? <CheckCircle2 size={9} /> : <XCircle size={9} />}
                        &nbsp;{statusPal.label}
                      </HudChip>
                      {approval.reviewer && <span>by {approval.reviewer}</span>}
                    </div>
                  )
                }
              >
                <div className={styles.body}>
                  <p className={styles.desc}>{approval.description}</p>
                  <div className={styles.chipRow}>
                    <HudChip color={statusPal.color}>{statusPal.label}</HudChip>
                    <HudChip color={priorityPal.color}>{priorityPal.label}</HudChip>
                    <span className={styles.requester}>
                      <User size={10} /> {approval.requester}
                    </span>
                    <span className={styles.timestamp}>
                      <Clock size={10} /> {formatDate(approval.createdAt)}
                    </span>
                  </div>
                </div>
              </HudPanel>
            );
          })}
        </div>
      )}

      <Modal
        isOpen={reviewModal !== null}
        onClose={closeReview}
        title={reviewModal?.action === 'approved' ? 'Approve Request' : 'Reject Request'}
      >
        {reviewModal && (
          <div className={styles.modalForm}>
            <div>
              <div className={styles.modalTitle}>{reviewModal.approval.title}</div>
              <p className={styles.modalDesc}>{reviewModal.approval.description}</p>
            </div>
            <div>
              <label className={styles.modalLabel} htmlFor="review-comment">
                Comment (optional)
              </label>
              <textarea
                id="review-comment"
                className={styles.textarea}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={reviewModal.action === 'approved'
                  ? 'Looks good, approved...'
                  : 'Reason for rejection...'}
              />
            </div>
            <div className={styles.modalActions}>
              <button
                type="button"
                className={styles.modalCancel}
                onClick={closeReview}
                disabled={submitting}
              >
                Cancel
              </button>
              <button
                type="button"
                className={`${styles.modalConfirm} ${
                  reviewModal.action === 'approved' ? styles.confirmApprove : styles.confirmReject
                }`}
                onClick={() => void handleSubmitReview()}
                disabled={submitting}
              >
                {submitting
                  ? <><Clock size={12} /> Processing...</>
                  : reviewModal.action === 'approved'
                    ? <><CheckCircle2 size={12} /> Confirm Approve</>
                    : <><XCircle size={12} /> Confirm Reject</>}
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
