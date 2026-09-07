"use client";

// The console is a static site; all calls go straight to the coordinator from the browser.
// Config (coordinator URL, admin token, a consumer key for the playground) lives in
// localStorage — this is a single-operator tool until real auth lands in M9.

const LS = {
  base: "sc.coordinatorUrl",
  admin: "sc.adminToken",
  consumer: "sc.consumerKey",
};

export function getCfg() {
  if (typeof window === "undefined") return { base: "", admin: "", consumer: "" };
  return {
    base:
      localStorage.getItem(LS.base) ||
      process.env.NEXT_PUBLIC_COORDINATOR_URL ||
      "https://api.ayni-ai.com",
    admin: localStorage.getItem(LS.admin) || "",
    consumer: localStorage.getItem(LS.consumer) || "",
  };
}

export function setCfg(p: { base?: string; admin?: string; consumer?: string }) {
  if (p.base !== undefined) localStorage.setItem(LS.base, p.base.replace(/\/+$/, ""));
  if (p.admin !== undefined) localStorage.setItem(LS.admin, p.admin);
  if (p.consumer !== undefined) localStorage.setItem(LS.consumer, p.consumer);
}

export function hasAdmin() {
  return !!getCfg().admin;
}

async function adminFetch(path: string, init?: RequestInit) {
  const { base, admin } = getCfg();
  if (!admin) throw new Error("set an admin token in Settings");
  const res = await fetch(base + path, {
    ...init,
    headers: { "X-Admin-Token": admin, "Content-Type": "application/json", ...(init?.headers || {}) },
  });
  if (!res.ok) throw new Error(`${res.status} ${(await res.text()) || res.statusText}`);
  return res.status === 204 ? null : res.json();
}

async function consumerFetch(path: string, init?: RequestInit) {
  const { base, consumer } = getCfg();
  if (!consumer) throw new Error("set a consumer API key in Settings");
  const res = await fetch(base + path, {
    ...init,
    headers: {
      Authorization: `Bearer ${consumer}`,
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
  });
  if (!res.ok) throw new Error(`${res.status} ${(await res.text()) || res.statusText}`);
  return res.json();
}

export type Device = {
  id: string;
  static_pk: string;
  kind: "phone" | "pc" | "other";
  platform: string;
  arch: string;
  cpu?: string;
  backend: string;
  hardware_class: string;
  trust_tier: string;
  models: string[];
  max_context: number;
  active_jobs: number;
  draining: boolean;
  ram_mb: number;
  thermal_state: string;
  security: Record<string, boolean>;
  telemetry: {
    active_jobs: number;
    queue_depth: number;
    cpu_load: number;
    mem_available_mb: number;
    thermal_state: string;
    battery_pct?: number | null;
    charging?: boolean | null;
    network?: string;
  };
  connected_at: string;
  last_seen: string;
  connected_for: string;
  owner?: { user_id: string; email: string; name: string };
  acu: number;
  class: string;
  decode_tps: number;
  sustained_end_tps: number;
  cpu_cores: number;
  benchmark_at?: string;
  jobs_unpaid: number;
  owed_usd: number;
};

export type Event = {
  seq: number;
  ts: string;
  level: "DEBUG" | "INFO" | "WARN" | "ERROR" | "AUDIT";
  msg: string;
  attrs?: Record<string, string>;
};

export const api = {
  models: async () => {
    const { base, consumer } = getCfg();
    const res = await fetch(base + "/v1/models", {
      headers: consumer ? { Authorization: `Bearer ${consumer}` } : {},
    });
    if (!res.ok) throw new Error(`${res.status}`);
    return res.json();
  },
  health: async () => {
    const { base } = getCfg();
    return (await fetch(base + "/healthz")).json();
  },

  // IT-admin console
  overview: () => adminFetch("/admin/overview"),
  providers: (): Promise<{ providers: Device[]; count: number }> => adminFetch("/admin/providers"),
  disconnectProvider: (id: string, reason: string) =>
    adminFetch(`/admin/providers/${encodeURIComponent(id)}/disconnect`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    }),
  drain: () => adminFetch("/admin/drain", { method: "POST" }),
  users: () => adminFetch("/admin/users"),
  events: (limit = 200, level = "", q = "", sinceSeq = 0): Promise<{ events: Event[]; stored: number; warnings_1h: number; errors_1h: number }> =>
    adminFetch(
      `/admin/events?limit=${limit}&level=${encodeURIComponent(level)}&q=${encodeURIComponent(q)}&since_seq=${sinceSeq}`,
    ),
  intake: () => adminFetch("/admin/intake"),
  setIntake: (paused: boolean, message: string) =>
    adminFetch("/admin/intake", { method: "POST", body: JSON.stringify({ paused, message }) }),
  grantCredit: (key_id: string, amount_usd: number, note: string) =>
    adminFetch("/admin/credit", { method: "POST", body: JSON.stringify({ key_id, amount_usd, note }) }),
  config: () => adminFetch("/admin/config"),
  metricsText: async () => {
    const { base, admin } = getCfg();
    const res = await fetch(base + "/metrics", { headers: admin ? { "X-Admin-Token": admin } : {} });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.text();
  },

  nodes: () => adminFetch("/admin/nodes"),
  usage: (hours = 24) => adminFetch(`/admin/usage?since_hours=${hours}`),
  earnings: (hours = 168) => adminFetch(`/admin/earnings?since_hours=${hours}`),
  waitlist: () => adminFetch("/admin/waitlist"),

  // billing (consumer key)
  balance: () => consumerFetch("/billing/balance"),
  topup: (amountUsd: number) =>
    consumerFetch("/billing/checkout", {
      method: "POST",
      body: JSON.stringify({ amount_usd: amountUsd }),
    }),

  // payouts (admin token)
  payoutsPending: () => adminFetch("/admin/payouts/pending"),
  payoutConnect: (staticPk: string, email: string) =>
    adminFetch("/admin/payouts/connect", {
      method: "POST",
      body: JSON.stringify({ static_pk: staticPk, email }),
    }),
  payoutRefresh: (staticPk: string) =>
    adminFetch("/admin/payouts/connect/refresh", {
      method: "POST",
      body: JSON.stringify({ static_pk: staticPk }),
    }),
  payoutsRun: (commit: boolean, minUsd = 0) =>
    adminFetch(`/admin/payouts/run?commit=${commit ? 1 : 0}&min_usd=${minUsd}`, {
      method: "POST",
    }),
  keys: () => adminFetch("/admin/keys"),
  createKey: (label: string) =>
    adminFetch("/admin/keys", { method: "POST", body: JSON.stringify({ label }) }),
  setKey: (id: string, disabled: boolean) =>
    adminFetch(`/admin/keys/${id}/${disabled ? "disable" : "enable"}`, { method: "POST" }),
};

// Small formatting helpers shared by the pages.
export const fmt = {
  gb: (mb: number) => (mb >= 1024 ? (mb / 1024).toFixed(mb >= 10240 ? 0 : 1) + " GB" : mb + " MB"),
  n: (v: number | undefined | null) => (v ?? 0).toLocaleString(),
  usd: (v: number | undefined | null, d = 2) => "$" + (v ?? 0).toFixed(d),
  ago: (iso?: string) => {
    if (!iso) return "—";
    const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 60) return `${Math.round(s)}s ago`;
    if (s < 3600) return `${Math.round(s / 60)}m ago`;
    if (s < 86400) return `${Math.round(s / 3600)}h ago`;
    return `${Math.round(s / 86400)}d ago`;
  },
  time: (iso?: string) => (iso ? new Date(iso).toLocaleTimeString() : "—"),
  dur: (s: number) => {
    if (s < 60) return `${s}s`;
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
    return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
  },
  kind: (k: string) => (k === "phone" ? "Phone" : k === "pc" ? "PC / laptop" : "Other"),
};

// Streaming chat for the playground. Calls onDelta with each token.
export async function chatStream(
  model: string,
  messages: { role: string; content: string }[],
  onDelta: (s: string) => void,
  signal?: AbortSignal,
) {
  const { base, consumer } = getCfg();
  if (!consumer) throw new Error("set a consumer API key in Settings");
  const res = await fetch(base + "/v1/chat/completions", {
    method: "POST",
    signal,
    headers: { Authorization: `Bearer ${consumer}`, "Content-Type": "application/json" },
    body: JSON.stringify({ model, messages, stream: true }),
  });
  if (!res.ok || !res.body) throw new Error(`${res.status} ${await res.text()}`);
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const parts = buf.split("\n\n");
    buf = parts.pop() || "";
    for (const p of parts) {
      const line = p.replace(/^data: /, "").trim();
      if (!line || line === "[DONE]") continue;
      let j: any;
      try {
        j = JSON.parse(line);
      } catch {
        continue; // keep-alive / comment frame
      }
      if (j.error) {
        // The coordinator reports a failed job as an SSE error frame; surface it
        // instead of leaving an empty bubble.
        throw new Error(j.error.code ? `${j.error.code}: ${j.error.message}` : j.error.message || "upstream error");
      }
      const d = j.choices?.[0]?.delta?.content;
      if (d) onDelta(d);
    }
  }
}
