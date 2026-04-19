/* ============================================================
   SOLDIER OF GOD — WebSocket Type Definitions
   ============================================================ */

export interface WebSocketEnvelope {
  type: string;
  timestamp: string;
  payload: unknown;
}

export type WebSocketStatus =
  | 'connecting'
  | 'connected'
  | 'disconnected'
  | 'error';
