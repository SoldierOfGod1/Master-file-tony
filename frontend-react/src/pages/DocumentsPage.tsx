/* ============================================================
   DocumentsPage — HUD library of project documents.
   Single wide panel with a searchable/filtered table, plus a
   modal for full content. Modal + data flow preserved.
   ============================================================ */

import { useState, useMemo, useCallback } from 'react';
import {
  FileStack,
  FileText,
  BookOpen,
  ClipboardList,
  File,
  FolderOpen,
  Calendar,
  User,
  type LucideIcon,
} from 'lucide-react';
import Modal from '../components/shared/Modal';
import HudPanel from '../components/shared/HudPanel';
import HudSummaryStrip from '../components/shared/HudSummaryStrip';
import { HudChip, HudStatusLed } from '../components/shared/HudChip';
import { useCommandCentre } from '../context/CommandCentreContext';
import type { Document } from '../types/api';
import hudStyles from '../theme/hud.module.css';
import styles from './DocumentsPage.module.css';

const FILTERS = ['All', 'ADR', 'Guide', 'Spec'] as const;
type Filter = (typeof FILTERS)[number];

interface TypeMeta {
  readonly icon: LucideIcon;
  readonly color: string;
  readonly label: string;
}
const TYPE_META: Record<string, TypeMeta> = {
  adr:   { icon: FileText,      color: '#ff7de0', label: 'ADR' },
  guide: { icon: BookOpen,      color: '#6ff2a0', label: 'Guide' },
  spec:  { icon: ClipboardList, color: '#7cc6ff', label: 'Spec' },
};
const metaFor = (t: string): TypeMeta =>
  TYPE_META[t.toLowerCase()] ?? { icon: File, color: '#ffaa00', label: t };

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('en-ZA', {
      day: '2-digit', month: 'short', year: 'numeric',
    });
  } catch {
    return iso;
  }
}

export default function DocumentsPage() {
  const { state } = useCommandCentre();
  const [activeFilter, setActiveFilter] = useState<Filter>('All');
  const [selectedDoc, setSelectedDoc] = useState<Document | null>(null);

  const filtered = useMemo(() => {
    if (activeFilter === 'All') return state.documents;
    return state.documents.filter((d) => d.type.toLowerCase() === activeFilter.toLowerCase());
  }, [state.documents, activeFilter]);

  const counts = useMemo(() => {
    const m: Record<string, number> = { adr: 0, guide: 0, spec: 0, other: 0 };
    for (const d of state.documents) {
      const k = d.type.toLowerCase();
      if (TYPE_META[k]) m[k] = (m[k] ?? 0) + 1;
      else m.other = (m.other ?? 0) + 1;
    }
    return m;
  }, [state.documents]);

  const openDoc = useCallback((d: Document) => setSelectedDoc(d), []);
  const closeDoc = useCallback(() => setSelectedDoc(null), []);

  return (
    <div className={hudStyles.page}>
      <HudSummaryStrip
        title="Documents Library"
        subtitle={`${state.documents.length} documents · ${Object.keys(TYPE_META).length} type categories`}
        gaugeValue={state.documents.length === 0 ? 0 : Math.min(state.documents.length / 50, 1)}
        gaugeReadout={`${state.documents.length}`}
        gaugeLabel="DOCS"
        gaugeColor="#00f0ff"
        segments={Object.entries(TYPE_META).map(([k, m]) => ({
          label: m.label, value: counts[k] ?? 0, color: m.color,
        }))}
        extra={
          <div className={styles.libraryIcon}>
            <FileStack size={22} style={{ color: '#00f0ff' }} />
          </div>
        }
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
        <HudPanel title="Library" accent="#7cc6ff" leading={<HudStatusLed color="#7cc6ff" animate={false} />}>
          <div className={styles.empty}>
            <FolderOpen size={36} className={styles.emptyIcon} />
            <span>No {activeFilter.toLowerCase()} documents</span>
          </div>
        </HudPanel>
      ) : (
        <HudPanel
          title={activeFilter === 'All' ? 'All Documents' : `${activeFilter} Documents`}
          accent="#00f0ff"
          leading={<HudStatusLed color="#6ff2a0" />}
          meta={<>{filtered.length}</>}
          footer={<>// click any row to view · sorted by most recent</>}
        >
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th></th>
                  <th>Title</th>
                  <th>Type</th>
                  <th>Version</th>
                  <th>Author</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((doc) => {
                  const m = metaFor(doc.type);
                  const Icon = m.icon;
                  return (
                    <tr
                      key={doc.id}
                      onClick={() => openDoc(doc)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') openDoc(doc);
                      }}
                      role="button"
                      tabIndex={0}
                    >
                      <td>
                        <div
                          className={styles.docIcon}
                          style={{ color: m.color, borderColor: `${m.color}66`, background: `${m.color}18` }}
                        >
                          <Icon size={14} />
                        </div>
                      </td>
                      <td>
                        <div className={styles.docTitle}>{doc.title}</div>
                        <div className={styles.docProject}>{doc.projectId}</div>
                      </td>
                      <td><HudChip color={m.color}>{m.label}</HudChip></td>
                      <td><span className={styles.version}>v{doc.version}</span></td>
                      <td className={styles.author}><User size={10} /> {doc.author}</td>
                      <td className={styles.date}><Calendar size={10} /> {formatDate(doc.createdAt)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </HudPanel>
      )}

      <Modal
        isOpen={selectedDoc !== null}
        onClose={closeDoc}
        title={selectedDoc?.title ?? 'Document'}
      >
        {selectedDoc && (
          <div>
            <div className={styles.modalMeta}>
              <HudChip color={metaFor(selectedDoc.type).color}>
                {metaFor(selectedDoc.type).label}
              </HudChip>
              <span className={styles.version}>v{selectedDoc.version}</span>
              <span className={styles.author}>
                <User size={10} /> {selectedDoc.author}
              </span>
              <span className={styles.date}>
                <Calendar size={10} /> {formatDate(selectedDoc.createdAt)}
              </span>
            </div>
            {selectedDoc.content ? (
              <pre className={styles.docContent}>{selectedDoc.content}</pre>
            ) : (
              <div className={styles.noContent}>// no content available for this document</div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
