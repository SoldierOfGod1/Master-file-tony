import type { ReactNode } from 'react';
import styles from './GlassCard.module.css';

interface GlassCardProps {
  readonly children: ReactNode;
  readonly className?: string;
  readonly title?: string;
  readonly icon?: ReactNode;
}

export default function GlassCard({
  children,
  className,
  title,
  icon,
}: GlassCardProps) {
  return (
    <div className={`${styles.card} ${className ?? ''}`}>
      {title && (
        <div className={styles.titleRow}>
          {icon && <span className={styles.titleIcon}>{icon}</span>}
          <h3 className={styles.title}>{title}</h3>
        </div>
      )}
      {children}
    </div>
  );
}
