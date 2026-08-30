"use client";

// The site is fully static. Forms post directly to the coordinator's public,
// unauthenticated intake endpoints (rate-limited, honeypot-protected server-side).
export const COORDINATOR =
  process.env.NEXT_PUBLIC_COORDINATOR_URL || "https://api.ayni-ai.com";

export const REPO = "https://github.com/mcastroarroyo/shared-compute";
export const WHITEPAPER = `${REPO}/blob/main/WHITEPAPER.md`;
export const LICENSE = `${REPO}/blob/main/LICENSE`;

async function post(path: string, body: unknown) {
  const res = await fetch(COORDINATOR + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (res.status === 429) throw new Error("Too many submissions — try again in a minute.");
  if (!res.ok) throw new Error((await res.text()) || `Error ${res.status}`);
  return res.json().catch(() => ({}));
}

export type Interest = "share" | "rent" | "both" | "initiatives";

export function joinWaitlist(input: {
  email: string;
  interest: Interest;
  note?: string;
  hp?: string; // honeypot — must be empty
}) {
  return post("/waitlist", input);
}

export function submitInitiative(input: {
  name: string;
  email: string;
  title: string;
  summary: string;
  link?: string;
  hp?: string;
}) {
  return post("/initiatives", input);
}

export type QuotePreview = {
  object: "workload.preview";
  model: string;
  tier: string;
  redundancy: number;
  supply_online: boolean;
  estimate: {
    items: number;
    prompt_tokens: number;
    completion_tokens: number;
    eligible_nodes: number;
    aggregate_tps: number;
    eta_seconds: number;
  };
  price: {
    currency: string;
    total_usd: number;
    breakdown_usd: {
      compute_acquisition: number;
      coordination: number;
      expected_failure: number;
      payment_processing: number;
      ayni_margin: number;
    };
  };
};

// Live, unauthenticated quote preview — estimate only, nothing stored or run.
export function quotePreview(input: {
  model: string;
  estimate: { count: number; avg_prompt_tokens: number; avg_completion_tokens: number };
  redundancy?: number;
}): Promise<QuotePreview> {
  return post("/v1/quote", input) as Promise<QuotePreview>;
}
