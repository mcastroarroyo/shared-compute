# Demo provider node

Always-on compute so every click on **demo.ayni-ai.com** is a real, encrypted
job. Keep one or two of these alive; if they all drop, the demo page falls back
to showing the last genuine run (labelled, never faked).

## Run it on a small VPS

Any ~2 vCPU / 4 GB Linux box with Docker (Hetzner CX22, DigitalOcean, Fly
machine, a spare desktop):

```bash
git clone https://github.com/mcastroarroyo/shared-compute
cd shared-compute/infra/demo-provider
cp .env.example .env
$EDITOR .env                     # set SC_REGISTRATION_TOKEN
docker compose up -d --build
docker compose logs -f           # expect "registered" then heartbeats
```

The container pulls the prebuilt `provider-daemon-linux-x64` and the 0.5B GGUF
via `https://api.ayni-ai.com/install/provider.sh` into a named volume, so
restarts are instant. Two nodes:

```bash
docker compose up -d --scale provider=2
```

## Getting a token

- **Per-account:** sign in at app.ayni-ai.com → *Share your computer* → copy the
  `sc_prov_…` token. Jobs served by this node accrue to that account.
- **Shared:** any value in the coordinator's `SC_PROVIDER_TOKENS` secret.

## No VPS?

Your Mac or an Android phone running the one-liner from the *Share* page is also
valid demo capacity — it just has to be connected while someone is trying the
demo. The Pixel connects as `device_attested`, which makes the "served by an
Android phone · device_attested" caption on the demo page true.

## Turn the demo on

```bash
fly secrets set --app ayni-coordinator SC_DEMO_ENABLED=1
```
