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
  providers: () => adminFetch("/admin/providers"),
  usage: (hours = 24) => adminFetch(`/admin/usage?since_hours=${hours}`),
  earnings: (hours = 168) => adminFetch(`/admin/earnings?since_hours=${hours}`),

  // billing (consumer key)
  balance: () => consumerFetch("/billing/balance"),
  topup: (amountUsd: number) =>
    consumerFetch("/billing/checkout", {
      method: "POST",
      body: JSON.stringify({ amount_usd: amountUsd }),
    }),

  // payouts (admin token)
  payoutsPending: () => adminFetch("/admin/payouts/pending"),
  payoutConnect: (staticPk: string) =>
    adminFetch("/admin/payouts/connect", {
      method: "POST",
      body: JSON.stringify({ static_pk: staticPk }),
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
      try {
        const j = JSON.parse(line);
        const d = j.choices?.[0]?.delta?.content;
        if (d) onDelta(d);
      } catch {
        /* ignore keep-alives */
      }
    }
  }
}
