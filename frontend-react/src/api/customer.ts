/* Customer 360 API — Axiom-backed client lookup.
   The backend handles query parameterisation; we just pass the raw
   phone/email the user typed. An optional `connectionID` argument picks a
   non-primary DB connection when the user has multiple clusters set up. */

import { apiGet } from './client';
import type { Customer360, CustomerConfig } from '../types/api';

export async function getCustomerConfig(): Promise<CustomerConfig | null> {
  return apiGet<CustomerConfig>('/customer/config');
}

/** `&connection=<id>` query segment; empty when the primary should be used. */
function connParam(connectionID?: string): string {
  return connectionID ? `&connection=${encodeURIComponent(connectionID)}` : '';
}

export async function lookupByPhone(phone: string, connectionID?: string): Promise<Customer360 | null> {
  return apiGet<Customer360>(
    `/customer?phone=${encodeURIComponent(phone)}${connParam(connectionID)}`,
  );
}

export async function lookupByEmail(email: string, connectionID?: string): Promise<Customer360 | null> {
  return apiGet<Customer360>(
    `/customer?email=${encodeURIComponent(email)}${connParam(connectionID)}`,
  );
}

export async function getCustomerByID(id: string, connectionID?: string): Promise<Customer360 | null> {
  const qs = connectionID ? `?connection=${encodeURIComponent(connectionID)}` : '';
  return apiGet<Customer360>(`/customer/${encodeURIComponent(id)}${qs}`);
}
