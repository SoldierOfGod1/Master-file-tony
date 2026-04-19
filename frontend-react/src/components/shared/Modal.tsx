import { useEffect, useCallback, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';
import styles from './Modal.module.css';

interface ModalProps {
  readonly isOpen: boolean;
  readonly onClose: () => void;
  readonly title?: string;
  readonly children: ReactNode;
}

export default function Modal({ isOpen, onClose, title, children }: ModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);

  const handleEscape = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    },
    [onClose],
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleEscape);
      return () => document.removeEventListener('keydown', handleEscape);
    }
  }, [isOpen, handleEscape]);

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.target === overlayRef.current) {
        onClose();
      }
    },
    [onClose],
  );

  if (!isOpen) return null;

  return (
    <div
      ref={overlayRef}
      className={styles.overlay}
      onClick={handleOverlayClick}
    >
      <div className={styles.content}>
        <div className={styles.header}>
          {title && <h2 className={styles.title}>{title}</h2>}
          <button
            className={styles.closeBtn}
            onClick={onClose}
            aria-label="Close modal"
            type="button"
          >
            <X size={18} />
          </button>
        </div>
        <div className={styles.body}>{children}</div>
      </div>
    </div>
  );
}
