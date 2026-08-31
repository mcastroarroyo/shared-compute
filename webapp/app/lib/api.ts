"use client";

// The signed-in app talks to the coordinator with the session cookie
// (Domain=.ayni-ai.com), so every call is credentialed.
export const API =
  process.env.NEXT_PUBLIC_API_URL || "https://api.ayni-ai.com";

async function req(path: string, init?: RequestInit) {
  const res = await fetch(API + path, {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(body?.error?.message || body?.error || body?.message || `HTTP ${res.status}`);
    // @ts-expect-error attach
    err.status = res.status;
    // @ts-expect-error attach
    err.code = body?.error?.code || body?.error || "";
    // @ts-expect-error attach
    err.body = body;
    throw err;
  }
  return body;
}

export type Me = {
  user: { id: string; email: string; name: string; avatar_url: string };
  api_key_id: string;
  provider_token: string;
  credit_usd: number;
};

export const api = {
  providers: (): Promise<{ providers: string[] }> => req("/auth/providers"),
  me: (): Promise<Me> => req("/v1/me"),
  logout: () => req("/auth/logout", { method: "POST" }),

  quoteWorkload: (input: {
    model: string;
    items: { messages: { role: string; content: string }[]; max_tokens?: number }[];
  }) => req("/v1/workloads", { method: "POST", body: JSON.stringify(input) }),

  acceptWorkload: (id: string) =>
    req(`/v1/workloads/${id}/accept`, { method: "POST" }),

  topup: (amountUsd: number): Promise<{ url?: string }> =>
    req("/billing/checkout", { method: "POST", body: JSON.stringify({ amount_usd: amountUsd }) }),

  earnings: (): Promise<{ owed_usd: number; jobs: number; devices: number }> =>
    req("/v1/me/earnings"),
};

export function signInURL(provider: string) {
  return `${API}/auth/${provider}`;
}
