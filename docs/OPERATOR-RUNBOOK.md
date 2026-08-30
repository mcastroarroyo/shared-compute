# Operator runbook

Everything here is done by the human operator. Claude prepares the code/config and walks
you through each screen, but you create accounts, enter payment details, pass ID/CAPTCHA
checks, and click the final deploy/publish buttons.

Rule of thumb: **never paste a secret into a chat, a commit, or a Dockerfile.** Secrets go
into the platform's own secret store (`fly secrets set`, Cloudflare dashboard, GitHub
Actions secrets).

---

## M2 — take the coordinator online

### 1. GitHub (you likely already have this)

- Create an **empty** repo `shared-compute` (private is fine).
- Back in the repo:
  ```bash
  git remote add origin git@github.com:<you>/shared-compute.git
  git push -u origin main
  ```

### 2. Fly.io — coordinator host

1. Sign up at <https://fly.io/app/sign-up>. Add a payment card (Billing → Add credit card).
   Expect ~$5–15/mo for one small machine.
2. Install the CLI: `brew install flyctl`
3. `fly auth login` (opens a browser).
4. From the repo root:
   ```bash
   fly launch --no-deploy --copy-config --dockerfile infra/coordinator.Dockerfile
   ```
   Accept the app name or set your own; pick a region near you.
5. Generate real credentials locally and load them as **secrets** (not committed):
   ```bash
   CONSUMER_KEY=sc_live_$(openssl rand -hex 24)
   PROVIDER_TOKEN=sc_prov_$(openssl rand -hex 24)
   fly secrets set SC_CONSUMER_API_KEYS="$CONSUMER_KEY" SC_PROVIDER_TOKENS="$PROVIDER_TOKEN"
   echo "SAVE THESE:  consumer=$CONSUMER_KEY  provider=$PROVIDER_TOKEN"
   ```
6. `fly deploy`
7. `curl https://<app>.fly.dev/healthz` → `{"status":"ok","providers":0}`

### 3. Cloudflare + domain

1. Sign up at <https://dash.cloudflare.com/sign-up> (free).
2. Get a domain: Cloudflare **Registrar** (Domain Registration → Register), or bring one
   you own and change its nameservers to Cloudflare.
3. In the zone for your domain: **DNS → Records → Add**
   - Type `CNAME`, Name `api`, Target `<app>.fly.dev`, Proxy **off** (grey cloud) for now.
4. Point Fly at the hostname and let it issue a cert:
   ```bash
   fly certs add api.<yourdomain>
   fly certs show api.<yourdomain>      # wait until it says "Ready"
   ```
5. `curl https://api.<yourdomain>/healthz`

### 4. Database (Fly Postgres)

```bash
fly postgres create --name shared-compute-db --region <same-as-app> --initial-cluster-size 1
fly postgres attach shared-compute-db --app <app>     # sets DATABASE_URL
fly secrets set SC_DATABASE_URL="$(fly ssh console -C 'printenv DATABASE_URL' --app <app>)"
```
Then `fly deploy` again. Migrations run automatically on boot.

### 5. Connect your Mac as the first provider

```bash
cd provider-core
cargo build --release -p provider-daemon --features llama
SC_COORDINATOR_URL="wss://api.<yourdomain>/ws/provider" \
SC_REGISTRATION_TOKEN="<the provider token from step 2.5>" \
SC_MODEL="qwen2.5-0.5b-instruct-q4_k_m" \
SC_MODEL_PATH="../models/qwen2.5-0.5b-instruct-q4_k_m.gguf" \
SC_BACKEND="llama" \
  ./target/release/provider-daemon
```

Smoke test from anywhere:
```bash
curl -N https://api.<yourdomain>/v1/chat/completions \
  -H "Authorization: Bearer <the consumer key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","stream":true,
       "messages":[{"role":"user","content":"say hi in 5 words"}]}'
```

---

## M3 — model registry (Cloudflare R2)

1. Cloudflare dashboard → **R2** → *Purchase R2* (needs the card; ~$0.015/GB-mo, no egress).
2. **Create bucket** `models`.
3. **Manage R2 API Tokens** → *Create API token* → Object Read & Write, scoped to `models`.
   Copy the Access Key ID + Secret.
4. R2 → bucket → Settings → **Public access / custom domain** → connect `models.<yourdomain>`.
5. Give the keys to Claude's upload step via env (local shell only):
   ```bash
   export R2_ACCOUNT_ID=... R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=...
   ```

## M4 — console

- **Vercel** (<https://vercel.com/signup>) or **Cloudflare Pages** — connect the GitHub repo,
  set the project root to `console/`.
- **Clerk** (<https://dashboard.clerk.com>) or Auth.js — create an application, copy the
  publishable + secret keys into the host's env vars.

## M5 — Android

- **Google Play Console**: <https://play.google.com/console/signup> — **$25 one-time**,
  government ID, a card in your legal name. Prepaid cards rejected. Non-refundable.
- Create the app, then the **internal testing** track; add tester emails (up to 12 without
  extra review).

## M7 — Windows code signing

- Buy an **OV or EV code-signing certificate** (Sectigo / DigiCert / SSL.com), ~$200–400/yr.
- Complete their business/identity vetting (callback + documents).
- Provide the `.pfx` + password to CI as encrypted secrets.

## M8 — confidential compute

- A cloud account offering **confidential VMs with attestable NVIDIA CC GPUs**
  (e.g. Azure NCC-H100-v5, GCP Confidential GPU). Needs a card; usage is $/hour.

## M9 — payouts, audit, legal

- **Stripe** (<https://dashboard.stripe.com/register>) — business verification for Connect.
  Claude builds the integration; **you** run onboarding and approve every payout batch.
- Engage a **penetration-test vendor** and **legal counsel** (ToS/privacy + the Darkbloom
  competition-clause review from `docs/LEGAL.md`).
