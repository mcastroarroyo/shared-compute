# Ayni tester program

How we find testers, bring them in, talk to them in one place, and turn what they say into
releases. Everything here is operated from the console's **Feedback** page plus one chat channel.

## The loop

1. **Invite** (posts below, or a personal message) → tester lands on `app.ayni-ai.com/testers`.
2. **Get in**: sign in with GitHub/Google. Android testers press *Request Android access*;
   the operator adds the Google account email to the Play `founders` tester list
   (Play Console → Testing → Internal testing → Testers) and replies with the
   [tester link](https://play.google.com/apps/internaltest/4701333072640107331).
3. **Try two things**: *Add a device* (phone: 6-letter pairing code; Mac/Linux: one command)
   and *Run a workload*.
4. **Tell us**: the feedback form (kind: bug / feedback / idea) → `POST /v1/feedback` →
   console **Feedback** page (filter, search, CSV export) + an INFO event in **Events**.
   The phone app's Settings links to the same page.
5. **Answer within a day** in the chat channel or by mail; label the item; fix; ship;
   tell the tester it shipped. Weekly: one release note in the channel.

## One channel

Pick one and put its invite link in `NEXT_PUBLIC_TESTER_CHAT_URL` for the web app
(Cloudflare Pages → ayni-app → Settings → Environment variables), then redeploy. The
Testers page shows a "Tester chat" card only when the variable is set.

Recommended: a **Discord server** with three channels: `#announcements` (release notes,
read-only), `#help` (pairing / install questions), `#feedback` (everything else). Discord
is free, needs no phone number for testers, and its invite links can be revoked.
Alternative for a very small group: a Telegram group.

The operator creates the server (Discord does not allow bots to create servers); paste the
invite here and in the env var. Suggested welcome message is at the end of this file.

## Where to find testers

Ranked by fit. Post the draft that matches each place; keep the honest framing
(internal test, one small model, earnings are cents) because that is what earns trust
with these audiences.

| Place | Why | Draft |
|---|---|---|
| r/LocalLLaMA | People who already run llama.cpp on their own hardware; care about privacy and open source | A |
| r/androidapps, r/Android | Phone owners who install test apps | B |
| Hacker News, Show HN | Technical readers, open-source friendly; the security story lands | A (shortened) |
| r/selfhosted, r/homelab | Idle machines, comfortable with a one-line installer | A |
| Product Hunt (later, at open testing) | Broader audience, needs a polished listing | C |
| Personal network, founders' contacts | Highest response rate; ask for 10 minutes and a screenshot | D |
| Dev communities (Discord servers around llama.cpp, Ollama, Rust) | Peer review from people who know the stack | A |

Do not post where the rules forbid self-promotion without reading the rules first
(most subreddits allow a "Show" style post with a clear "I built this" and no
link-only posts). Reply to every comment in the first two hours.

## Drafts

### A. Technical communities (r/LocalLLaMA, HN, r/selfhosted)

**Title:** Ayni: an open-source marketplace that runs private LLM inference on idle phones and laptops, pays the owners 70%, and never logs a prompt. Looking for testers.

I built Ayni to answer a question that bothered me: billions of capable devices sit idle while inference runs in a few data centers. Ayni turns idle phones, laptops and workstations into a private inference network.

What is different:
- Every job is sealed to one device with a fresh X25519 key; the coordinator relays ciphertext. A CI check fails the build if any code path could log a prompt.
- Buyers can require hardware-attested devices (Android StrongBox key attestation, verified boot) with one header.
- One itemised quote for a whole batch, charged once. No bidding, no token, no blockchain. Stripe in, Stripe out.
- A fail-closed "Council" reviews each workload's shape (counts, tier, price), never its content, and publishes hash-chained decisions.
- Apache 2.0, Go coordinator + Rust provider on llama.cpp. `make dev`, `make e2e`.

Honest status: production runs one 0.5B model on consumer devices; a phone does ~18 tok/s, an M1 Pro ~90. A batch of 10 prompts across two phones and a Mac finishes in about 5 s. Earnings for one phone are cents. Larger classes are priced and scheduled but need GPU providers.

I am looking for testers with an Android phone or a Mac/Linux box: 10 minutes, one command or a 6-letter pairing code. Feedback goes straight to me.

app.ayni-ai.com/testers · code: github.com/mcastroarroyo/shared-compute · white paper linked from the site.

### B. Phone-owner communities (r/androidapps)

**Title:** [Testers wanted] Ayni: share your phone's idle time for private AI jobs, get paid per token. Android internal test.

Ayni is a small app that runs AI inference jobs on your phone only while it is charging and on Wi-Fi, and pays you a share of what each job was billed. Jobs are encrypted to your phone with a one-time key; nothing is stored, no ads, no tracking, no access to your files.

What I need: Android 8+ phones (Pixel and Samsung ideal, others welcome), 10 minutes, and honest feedback on the pairing flow: you sign in on a computer, get a 6-letter code, type it in the app, done.

Be aware: this is an internal test, one small model, and earnings today are cents. What you get is an early look and a direct line to the developer.

Request access at app.ayni-ai.com/testers (use your Google account email).

### C. Product Hunt (hold until open testing)

Tagline: The world's unused compute, on demand.
One-liner: Rent private AI inference from a network of idle phones and laptops, or share yours and earn 70% of every job.

### D. Personal message

Hi <name>, I built something and need ten minutes of your time. Ayni lets a phone or laptop earn money running small AI jobs, encrypted so nobody, including me, can read them. Would you install the app (or run one command on your Mac), pair it with a 6-letter code, and tell me what confused you? app.ayni-ai.com/testers has the steps. Screenshots of anything odd are gold. Thank you.

## Welcome message for the channel

Welcome to the Ayni tester channel. Three things:
1. `app.ayni-ai.com/testers` has the steps: sign in, add a device (phone code or one command), run a workload.
2. Post bugs and confusion in `#feedback`, questions in `#help`. Screenshots and the exact text on screen help most.
3. Release notes land in `#announcements`. Your device's earnings are on `app.ayni-ai.com/earnings`; during the test they are real but small.

Rules: be kind, no prompts with personal data (the network is private by design but this is a test), and tell us if something feels wrong.

## Triage labels

`pairing`, `installer`, `phone-policy`, `quote`, `billing`, `earnings`, `docs`, `idea`.
A bug is fixed when the tester who reported it says so.
