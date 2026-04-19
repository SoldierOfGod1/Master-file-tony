/* ============================================================
   SOLDIER OF GOD — Clock Hook
   Returns current time string, updates every second
   ============================================================ */

import { useEffect, useState } from 'react';

function formatTime(date: Date): string {
  return date.toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

export function useClock(): string {
  const [time, setTime] = useState(() => formatTime(new Date()));

  useEffect(() => {
    const interval = setInterval(() => {
      setTime(formatTime(new Date()));
    }, 1_000);

    return () => clearInterval(interval);
  }, []);

  return time;
}
