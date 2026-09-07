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

  // Device onboarding: a short-lived pairing code the phone app redeems, and
  // the live list of this account's devices (online + offline).
  pairCode: (): Promise<{ code: string; expires_at: string; ttl_seconds: number }> =>
    req("/v1/me/pair", { method: "POST" }),
  devices: (): Promise<{ devices: MyDevice[]; online: number; total: number }> =>
    req("/v1/me/devices"),

  feedback: (input: { kind: string; message: string; email?: string; device?: string; app?: string; version?: string }) =>
    req("/v1/feedback", { method: "POST", body: JSON.stringify({ app: "webapp", ...input }) }),
  myFeedback: (): Promise<{ feedback: FeedbackThread[] }> => req("/v1/me/feedback"),
  replyFeedback: (id: number, body: string) =>
    req(`/v1/me/feedback/${id}/reply`, { method: "POST", body: JSON.stringify({ body }) }),
};

export type FeedbackThread = {
  id: number;
  kind: string;
  email: string;
  device: string;
  message: string;
  app: string;
  version: string;
  created_at: string;
  replies: { id: number; author: "ayni" | "tester"; body: string; created_at: string }[];
};

export type MyDevice = {
  static_pk: string;
  online: boolean;
  kind: "phone" | "pc" | "other";
  platform?: string;
  arch?: string;
  cpu?: string;
  trust_tier?: string;
  class?: string;
  acu: number;
  tps: number;
  ram_mb?: number;
  active_jobs: number;
  connected_for?: string;
  last_seen?: string;
  jobs_unpaid: number;
  owed_usd: number;
};

export function signInURL(provider: string) {
  return `${API}/auth/${provider}`;
}
