import type { CSSProperties } from 'react';

type StatusType = 'online' | 'offline' | 'idle' | 'active' | 'warning' | 'error';

interface StatusDotProps {
  readonly status: StatusType;
}

const STATUS_COLORS: Record<StatusType, string> = {
  online: 'var(--neon-green)',
  active: 'var(--neon-cyan)',
  idle: 'var(--neon-amber)',
  warning: 'var(--neon-amber)',
  offline: 'var(--text-muted)',
  error: 'var(--neon-red)',
} as const;

const STATUS_GLOWS: Record<StatusType, string> = {
  online: 'rgba(0, 255, 136, 0.5)',
  active: 'rgba(0, 240, 255, 0.5)',
  idle: 'rgba(255, 170, 0, 0.5)',
  warning: 'rgba(255, 170, 0, 0.5)',
  offline: 'transparent',
  error: 'rgba(255, 51, 85, 0.5)',
} as const;

const PULSING_STATUSES = new Set<StatusType>(['online', 'active', 'idle']);

const baseStyle: CSSProperties = {
  display: 'inline-block',
  width: 8,
  height: 8,
  borderRadius: '50%',
  flexShrink: 0,
};

export default function StatusDot({ status }: StatusDotProps) {
  const color = STATUS_COLORS[status];
  const glow = STATUS_GLOWS[status];
  const shouldPulse = PULSING_STATUSES.has(status);

  const dotStyle: CSSProperties = {
    ...baseStyle,
    background: color,
    boxShadow: `0 0 6px ${glow}`,
    animation: shouldPulse ? 'statusDotPulse 2s ease-in-out infinite' : 'none',
  };

  return (
    <>
      {shouldPulse && (
        <style>{`
          @keyframes statusDotPulse {
            0%, 100% { box-shadow: 0 0 4px ${glow}; }
            50% { box-shadow: 0 0 12px ${glow}, 0 0 24px ${glow}; }
          }
        `}</style>
      )}
      <span style={dotStyle} role="status" aria-label={status} />
    </>
  );
}
