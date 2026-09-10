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

## One channel (built)

Two surfaces, both operated from the console, no third-party accounts needed:

- **Private, per tester:** the form on `app.ayni-ai.com/testers` (also reachable from the phone app's
  "Send feedback about this app"). Each entry is a thread. The team replies from the console's
  **Feedback** page (inline reply box); the tester sees the reply under "Your messages" on the same
  page and can answer there. API: `POST /v1/feedback`, `GET /v1/me/feedback`,
  `POST /v1/me/feedback/{id}/reply`, `POST /admin/feedback/{id}/reply` (audited).
- **Public, all testers:** GitHub Discussions on the repo
  (https://github.com/mcastroarroyo/shared-compute/discussions). Testers already sign in to Ayni with
  GitHub, so there is nothing new to join. Welcome thread: Discussions #2 (Announcements).
  Release notes go in Announcements; questions in Q&A; ideas in Ideas.

The public site has a "Testers wanted" page at `ayni-ai.com/testers` (nav link) and the README has a
matching section. `NEXT_PUBLIC_TESTER_CHAT_URL` can override the community link if a chat server is
added later.

**Daily routine (operator or agent):** open the console Feedback page → reply to everything new →
label in the reply ("pairing", "installer", …) → fix → when it ships, post release notes in
Discussions → Announcements and reply to the original thread with "shipped in <version>".

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

## Drafts (tone: join the community now; workloads ramp as buyers arrive; cents today)

### A. r/LocalLLaMA (show and tell)

**Title:** Ayni: open-source network that runs small LLM jobs on idle phones and laptops, encrypted per job. Looking for early devices.

I have been building Ayni, an open-source (Apache 2.0) way for idle consumer devices to serve small-model inference for other people, and I am opening it to early testers.

How it works: your phone or laptop runs a small provider (llama.cpp inside). A coordinator quotes a batch job up front, seals each item to one device with a fresh key, relays ciphertext, and pays the device from what the job was billed. A CI check fails the build if any code path could log a prompt. Buyers can require hardware-attested phones.

Where it actually is today: one 0.5B model in production, a handful of devices online, and test jobs a few times a day. A phone does about 18 tok/s, an M-series Mac 80 to 160. Earnings are cents. Paid workloads ramp up as buyers arrive; right now you would be joining the community that makes the network real, not a paycheck.

What I need: Android phones (any 8+, Pixel and Samsung ideal) and Mac or Linux boxes. Ten minutes: sign in, one command or a six-letter pairing code, done. A test job reaches new devices within minutes so you can see it work. Feedback goes straight to me, in the app or on GitHub Discussions.

Code, threat model and white paper: github.com/mcastroarroyo/shared-compute · Join: app.ayni-ai.com/testers

### B. r/selfhosted, r/homelab

**Title:** Looking for idle Macs/Linux boxes to join an open-source community inference network (one command, no crypto, cents per job)

I built Ayni, an open-source coordinator plus provider that turns idle machines into a small private inference network. No token, no blockchain: buyers prepay dollars, devices are paid by Stripe.

The provider is one binary (Go coordinator, Rust provider with llama.cpp). Install is one command; it dials out over WebSocket, accepts no inbound connections, and only serves while your machine is idle. Jobs are encrypted to your machine's key and never logged.

Honest status: one 0.5B model, a handful of devices, test jobs a few times a day. Paid workloads ramp as buyers arrive, so today this is about being part of the community early and shaping it, not income.

Ten minutes to join: app.ayni-ai.com/testers. Code and docs: github.com/mcastroarroyo/shared-compute. I answer every message.

### C. r/androidapps

**Title:** [Testers wanted] Ayni: let your phone run small AI jobs while charging, open source, encrypted, cents per job

Ayni is a small Android app that runs AI inference jobs on your phone only while it is charging and on Wi-Fi, and pays a share of what each job billed. Jobs are encrypted to your phone with a one-time key; nothing is stored, no ads, no tracking, no access to your files. Open source.

Status, plainly: this is early. One small model, a few devices, and test jobs a few times a day while the buyer side grows. You would be joining the community that makes the network real; earnings today are cents.

What I need: Android 8+ phones, ten minutes, and honest feedback on the pairing flow (sign in on a computer, get a six-letter code, type it in the app). Testers get the Play link after signing in at app.ayni-ai.com/testers.

### D. Show HN

**Title:** Show HN: Ayni, an open-source network for private LLM inference on idle phones and laptops

Ayni turns idle consumer devices into a small inference network. A Go coordinator quotes a whole batch up front, seals each item to one device's X25519 key, relays ciphertext, verifies token counts itself, and pays the device owner from what the job billed, in dollars through Stripe. A fail-closed "Council" reviews each workload's shape (counts, tier, price), never its content, and publishes hash-chained decisions. Buyers can require hardware-attested Android devices.

Status: one 0.5B model in production, a handful of devices, test jobs a few times a day. Larger classes are priced and scheduled but need GPU providers, and paid workloads ramp as buyers arrive. I am looking for early devices (Android, Mac, Linux) and for people to read the threat model and tell me what is wrong with it.

Code: https://github.com/mcastroarroyo/shared-compute · Join: https://app.ayni-ai.com/testers

### E. Personal message

Hi <name>, I built Ayni, an open-source network that runs small AI jobs on idle phones and laptops, encrypted so nobody, including me, can read them. It is early: a few devices, test jobs a few times a day, earnings are cents. I am putting together the first community of devices and would love yours in it. Ten minutes: app.ayni-ai.com/testers. Tell me what confused you. Thank you.

## Scale path to 1,000 devices

- **Phones:** Play internal testing holds 100 testers by list. The open testing track (Testing → Open testing) lets anyone join from a Play link with no list: select countries, promote the internal release to it, send for review. Do this before posting to phone communities.
- **Macs and Linux boxes:** no store involved; the one-line installer has no cap.
- **Every new device gets a job within minutes:** `scripts/tester-pulse.py` runs every ten minutes from launchd on the founders' Mac (`~/Library/LaunchAgents/com.ayni.tester-pulse.plist`), sends a three-to-twelve item job whenever a device appears or returns, and caps spend at $0.50 a day. Log: `~/.ayni/pulse/pulse.jsonl`; `python3 scripts/tester-pulse.py --status`.
- **Funnel numbers:** `GET /admin/overview` → `users.total`, `users.with_devices`, `providers.known`, `providers.online`. Baseline 2026-09-10: 3 users, 2 with devices, 10 known devices, 1 online.

## Welcome message for the channel

Welcome to the Ayni tester channel. Three things:
1. `app.ayni-ai.com/testers` has the steps: sign in, add a device (phone code or one command), run a workload.
2. Post bugs and confusion in `#feedback`, questions in `#help`. Screenshots and the exact text on screen help most.
3. Release notes land in `#announcements`. Your device's earnings are on `app.ayni-ai.com/earnings`; during the test they are real but small.

Rules: be kind, no prompts with personal data (the network is private by design but this is a test), and tell us if something feels wrong.

## Triage labels

`pairing`, `installer`, `phone-policy`, `quote`, `billing`, `earnings`, `docs`, `idea`.
A bug is fixed when the tester who reported it says so.
