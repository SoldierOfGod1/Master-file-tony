/* ============================================================
   SOLDIER OF GOD — Keyboard Shortcuts Hook
   Registers global keydown listener from a key -> callback map
   ============================================================ */

import { useEffect, useRef } from 'react';

export function useKeyboardShortcuts(
  shortcuts: Record<string, () => void>,
): void {
  // Keep a stable ref so the listener always sees the latest map
  // without needing to re-register
  const shortcutsRef = useRef(shortcuts);
  shortcutsRef.current = shortcuts;

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      // Skip if user is typing in an input/textarea/contenteditable
      const target = event.target as HTMLElement;
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' ||
        target.isContentEditable
      ) {
        return;
      }

      const callback = shortcutsRef.current[event.key];
      if (callback) {
        event.preventDefault();
        callback();
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);
}
