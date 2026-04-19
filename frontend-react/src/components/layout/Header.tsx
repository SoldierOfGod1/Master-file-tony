import type { WebSocketStatus } from '../../types/websocket';
import styles from './Header.module.css';

interface HeaderProps {
  readonly clock: string;
  readonly gatewayStatus: WebSocketStatus;
}

function HexagonIcon() {
  return (
    <svg
      className={styles.hexIcon}
      width="32"
      height="36"
      viewBox="0 0 32 36"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M16 1L30 9.5V26.5L16 35L2 26.5V9.5L16 1Z"
        stroke="url(#hex-grad)"
        strokeWidth="2"
        fill="rgba(0, 119, 200, 0.1)"
      />
      <text
        x="16"
        y="21"
        textAnchor="middle"
        fill="#00f0ff"
        fontFamily="var(--font-display)"
        fontSize="10"
        fontWeight="700"
      >
        SOG
      </text>
      <defs>
        <linearGradient id="hex-grad" x1="2" y1="1" x2="30" y2="35">
          <stop stopColor="#0077C8" />
          <stop offset="1" stopColor="#00f0ff" />
        </linearGradient>
      </defs>
    </svg>
  );
}

export default function Header({ clock, gatewayStatus }: HeaderProps) {
  return (
    <header className={styles.header}>
      <div className={styles.left}>
        <HexagonIcon />
        <div className={styles.titleGroup}>
          <h1 className={styles.title}>SOLDIER OF GOD</h1>
          <span className={styles.subtitle}>
            AI Agent Command Centre &mdash; rain automation
          </span>
        </div>
      </div>

      <div className={styles.right}>
        <div className={styles.gatewayGroup}>
          <span
            className={`${styles.gatewayDot} ${
              gatewayStatus === 'connected'
                ? styles.gatewayOnline
                : styles.gatewayOffline
            }`}
          />
          <span className={styles.gatewayLabel}>
            Gateway {gatewayStatus === 'connected' ? 'Online' : 'Offline'}
          </span>
        </div>
        <span className={styles.clock}>{clock}</span>
      </div>
    </header>
  );
}
