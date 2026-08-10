import { QueryClient } from "@tanstack/react-query";

export const qc = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchInterval: false,
      refetchIntervalInBackground: false,
      refetchOnWindowFocus: true,
      retry: 1,
      placeholderData: (previous: unknown) => previous,
    },
  },
});

export class APIRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "APIRequestError";
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
    ...init,
  });
  const body = await r.json().catch(() => ({}));
  if (!r.ok)
    throw new APIRequestError(
      body?.error?.message || `HTTP ${r.status}`,
      r.status,
    );
  return body;
}

export async function publicAPI<T>(
  path: string,
  apiKey: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Authorization", `Bearer ${apiKey.trim()}`);
  if (!(init?.body instanceof FormData))
    headers.set("Content-Type", "application/json");
  const r = await fetch(path, { ...init, headers });
  const body = await r.json().catch(() => ({}));
  if (!r.ok)
    throw new Error(
      body?.error?.message || body?.message || `HTTP ${r.status}`,
    );
  return body;
}

export function idempotencyKey() {
  return (
    globalThis.crypto?.randomUUID?.() ||
    `playground-${Date.now()}-${Math.random().toString(16).slice(2)}`
  );
}
