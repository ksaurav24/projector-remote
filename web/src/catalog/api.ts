import { useCallback, useEffect, useState } from "react";

// Responses are cached for the life of the page so going back to a
// screen (or reopening a title) doesn't refetch. The catalog changes
// slowly; a reload of the PWA is a fresh start.
const cache = new Map<string, unknown>();

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  if (cache.has(url)) return cache.get(url) as T;
  const res = await fetch(url, { signal });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    throw new ApiError(res.status, body?.error ?? `Request failed (${res.status})`);
  }
  cache.set(url, body);
  return body as T;
}

/** Drops cached responses whose URL starts with prefix, e.g. after
 * playback changes what continue-watching or a title's progress says. */
export function invalidate(prefix: string) {
  for (const key of cache.keys()) {
    if (key.startsWith(prefix)) cache.delete(key);
  }
}

export function catalogUrl(route: string, params: Record<string, string | number>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) q.set(k, String(v));
  return `/api/catalog/${route}?${q}`;
}

type State<T> =
  | { status: "loading"; data?: undefined; error?: undefined }
  | { status: "ok"; data: T; error?: undefined }
  | { status: "error"; data?: undefined; error: string };

/** Fetches url (null = don't fetch yet) and tracks loading/error
 * state. retry() refetches after a failure. */
export function useApi<T>(url: string | null): State<T> & { retry: () => void } {
  const [attempt, setAttempt] = useState(0);
  const key = url ? `${url}#${attempt}` : null;
  const [result, setResult] = useState<{ key: string; data?: T; error?: string } | null>(null);

  useEffect(() => {
    if (!url || cache.has(url)) return;
    const ctrl = new AbortController();
    const k = `${url}#${attempt}`;
    getJSON<T>(url, ctrl.signal).then(
      (data) => setResult({ key: k, data }),
      (err) => {
        if (ctrl.signal.aborted) return;
        setResult({ key: k, error: err instanceof ApiError ? err.message : "Can't reach the projector" });
      },
    );
    return () => ctrl.abort();
  }, [url, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  let state: State<T> = { status: "loading" };
  if (url && cache.has(url)) state = { status: "ok", data: cache.get(url) as T };
  else if (result && result.key === key) {
    state = result.error !== undefined ? { status: "error", error: result.error } : { status: "ok", data: result.data as T };
  }
  return { ...state, retry };
}
