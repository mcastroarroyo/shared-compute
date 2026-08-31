"use client";

// The demo talks only to public, unauthenticated coordinator endpoints.
export const API =
  process.env.NEXT_PUBLIC_API_URL || "https://api.ayni-ai.com";

// Where the "book a call" button points (set at build time).
export const CALENDAR_URL = process.env.NEXT_PUBLIC_CALENDAR_URL || "";

export type Review = {
  seat: string;
  role: string;
  decision: string;
  severity: string;
};

export type DemoResponse = {
  object: "demo.summarize";
  quote: {
    currency: string;
    total_usd: number;
    eta_seconds: number;
    eligible_nodes: number;
    supply_online: boolean;
    breakdown_usd: Record<string, number>;
  };
  council: {
    decision: string;
    risk_class: string;
    reviews: Review[];
    required_controls: string[] | null;
    audit_hash: string;
    facts: Record<string, unknown>;
  };
  blocked?: boolean;
  error?: string;
  result: {
    summary: string;
    wall_ms: number;
    prompt_tokens: number;
    completion_tokens: number;
    would_cost_usd: number;
    device: Record<string, unknown>;
  } | null;
  fallback?: LastJob | null;
  record?: LastJob;
};

export type LastJob = {
  model: string;
  class: string;
  tier: string;
  prompt_tokens: number;
  completion_tokens: number;
  wall_ms: number;
  would_cost_usd: number;
  device: Record<string, unknown>;
  council_decision: string;
  audit_hash: string;
  recorded_at: string;
};

export async function summarize(text: string): Promise<DemoResponse> {
  const res = await fetch(API + "/v1/demo/summarize", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(body?.error?.message || body?.error || `HTTP ${res.status}`);
  }
  return body as DemoResponse;
}

export async function lastJob(): Promise<LastJob | null> {
  const res = await fetch(API + "/v1/demo/last-job");
  const body = await res.json().catch(() => ({}));
  return (body?.job as LastJob) ?? null;
}
