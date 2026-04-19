/* ============================================================
   SOLDIER OF GOD — Base API Client
   Typed fetch wrapper with consistent error handling
   ============================================================ */

interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

const API_BASE = '/api/v1';

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ApiResponse<T>> {
  const url = `${API_BASE}${path}`;

  const headers: Record<string, string> = {
    'Accept': 'application/json',
  };

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }

  try {
    const res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (!res.ok) {
      const text = await res.text().catch(() => '');
      return {
        success: false,
        error: text || `HTTP ${res.status} ${res.statusText}`,
      };
    }

    // Handle 204 No Content
    if (res.status === 204) {
      return { success: true };
    }

    const json = (await res.json()) as ApiResponse<T>;
    return json;
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Unknown network error';
    return { success: false, error: message };
  }
}

export async function apiGet<T>(path: string): Promise<T | null> {
  const res = await request<T>('GET', path);
  if (!res.success || res.data === undefined) {
    if (res.error) {
      console.error(`[API GET ${path}]`, res.error);
    }
    return null;
  }
  return res.data;
}

export async function apiPost<T>(path: string, body: unknown): Promise<T | null> {
  const res = await request<T>('POST', path, body);
  if (!res.success || res.data === undefined) {
    if (res.error) {
      console.error(`[API POST ${path}]`, res.error);
    }
    return null;
  }
  return res.data;
}

export async function apiPut<T>(path: string, body: unknown): Promise<T | null> {
  const res = await request<T>('PUT', path, body);
  if (!res.success || res.data === undefined) {
    if (res.error) {
      console.error(`[API PUT ${path}]`, res.error);
    }
    return null;
  }
  return res.data;
}

export async function apiPatch<T>(path: string, body: unknown): Promise<T | null> {
  const res = await request<T>('PATCH', path, body);
  if (!res.success || res.data === undefined) {
    if (res.error) {
      console.error(`[API PATCH ${path}]`, res.error);
    }
    return null;
  }
  return res.data;
}

export async function apiDelete(path: string): Promise<boolean> {
  const res = await request<void>('DELETE', path);
  if (!res.success) {
    if (res.error) {
      console.error(`[API DELETE ${path}]`, res.error);
    }
    return false;
  }
  return true;
}
