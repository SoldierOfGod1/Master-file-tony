/* ============================================================
   SOLDIER OF GOD — Documents API
   ============================================================ */

import type { Document } from '../types/api';
import { apiGet, apiPost, apiPut } from './client';

export async function listDocuments(): Promise<Document[]> {
  const data = await apiGet<Document[]>('/documents');
  return data ?? [];
}

export async function createDocument(
  doc: Omit<Document, 'id'>,
): Promise<Document | null> {
  return apiPost<Document>('/documents', doc);
}

export async function getDocument(id: string): Promise<Document | null> {
  return apiGet<Document>(`/documents/${encodeURIComponent(id)}`);
}

export async function updateDocument(
  id: string,
  updates: Partial<Document>,
): Promise<Document | null> {
  return apiPut<Document>(`/documents/${encodeURIComponent(id)}`, updates);
}
