/* ============================================================
   SOLDIER OF GOD — WebSocket Hook with Auto-Reconnect
   ============================================================ */

import { useCallback, useEffect, useRef, useState } from 'react';
import type { WebSocketEnvelope, WebSocketStatus } from '../types/websocket';

const MIN_RECONNECT_DELAY = 2_000;
const MAX_RECONNECT_DELAY = 30_000;

interface UseWebSocketReturn {
  status: WebSocketStatus;
  lastMessage: WebSocketEnvelope | null;
  send: (data: WebSocketEnvelope) => void;
  reconnect: () => void;
}

export function useWebSocket(url: string): UseWebSocketReturn {
  const [status, setStatus] = useState<WebSocketStatus>('disconnected');
  const [lastMessage, setLastMessage] = useState<WebSocketEnvelope | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectDelayRef = useRef(MIN_RECONNECT_DELAY);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);
  const urlRef = useRef(url);
  urlRef.current = url;

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current !== null) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const connect = useCallback(() => {
    if (!mountedRef.current) return;

    // Clean up any existing connection
    if (wsRef.current) {
      wsRef.current.onopen = null;
      wsRef.current.onclose = null;
      wsRef.current.onerror = null;
      wsRef.current.onmessage = null;
      if (wsRef.current.readyState === WebSocket.OPEN ||
          wsRef.current.readyState === WebSocket.CONNECTING) {
        wsRef.current.close();
      }
    }

    setStatus('connecting');

    try {
      const ws = new WebSocket(urlRef.current);
      wsRef.current = ws;

      ws.onopen = () => {
        if (!mountedRef.current) return;
        setStatus('connected');
        reconnectDelayRef.current = MIN_RECONNECT_DELAY;
      };

      ws.onmessage = (event: MessageEvent) => {
        if (!mountedRef.current) return;
        try {
          const envelope = JSON.parse(event.data as string) as WebSocketEnvelope;
          setLastMessage(envelope);
        } catch {
          console.warn('[WS] Failed to parse message:', event.data);
        }
      };

      ws.onerror = () => {
        if (!mountedRef.current) return;
        setStatus('error');
      };

      ws.onclose = () => {
        if (!mountedRef.current) return;
        setStatus('disconnected');

        // Schedule reconnect with exponential backoff
        const delay = reconnectDelayRef.current;
        reconnectDelayRef.current = Math.min(
          delay * 2,
          MAX_RECONNECT_DELAY,
        );

        clearReconnectTimer();
        reconnectTimerRef.current = setTimeout(() => {
          connect();
        }, delay);
      };
    } catch {
      setStatus('error');
    }
  }, [clearReconnectTimer]);

  const send = useCallback((data: WebSocketEnvelope) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data));
    } else {
      console.warn('[WS] Cannot send — not connected');
    }
  }, []);

  const reconnect = useCallback(() => {
    clearReconnectTimer();
    reconnectDelayRef.current = MIN_RECONNECT_DELAY;
    connect();
  }, [connect, clearReconnectTimer]);

  // Connect on mount, cleanup on unmount
  useEffect(() => {
    mountedRef.current = true;
    connect();

    return () => {
      mountedRef.current = false;
      clearReconnectTimer();
      if (wsRef.current) {
        wsRef.current.onopen = null;
        wsRef.current.onclose = null;
        wsRef.current.onerror = null;
        wsRef.current.onmessage = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [connect, clearReconnectTimer]);

  return { status, lastMessage, send, reconnect };
}
