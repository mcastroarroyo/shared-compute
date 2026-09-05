# Ayni on Google Cloud — production runbook

Project **`ayni1-507216`**, region **`us-east1`**. Replaces the Fly.io deployment of
the coordinator (`api.ayni-ai.com`). Web surfaces stay on Cloudflare Pages.

## Topology
| Piece | Resource | Notes |
|---|---|---|
| API | Cloud Run `ayni-coordinator` | min 1 instance (provider WebSockets), session affinity, 3600 s timeout, `--no-cpu-throttling` |
| DB | Cloud SQL PG17 `ayni-pg` | **private IP only**, PITR + 7 daily backups, deletion protection, IAM auth flag on |
| Secrets | Secret Manager | one secret per `SC_*` value; the service account holds only `secretAccessor` |
| Signing key | Cloud KMS `ayni/manifest-signer` (EC_SIGN_ED25519) | private key never leaves KMS; SA holds `signerVerifier` |
| Images | Artifact Registry `us-east1-docker.pkg.dev/ayni1-507216/ayni/coordinator` | built by Cloud Build from `infra/gcp/cloudbuild.yaml` |
| Identity | SA `ayni-coordinator@…` | roles: cloudsql.client, secretmanager.secretAccessor, cloudkms.signerVerifier/viewer, logging.logWriter, monitoring.metricWriter |
| Edge | Cloud Armor policy + global HTTPS LB (serverless NEG) | WAF preconfigured rules + per-IP rate limit in front of Cloud Run |

## Deploy
```bash
gcloud builds submit --config infra/gcp/cloudbuild.yaml --substitutions=_TAG=$(git rev-parse --short HEAD) .
infra/gcp/deploy.sh            # deploys the image tagged with the current git sha
```
Secrets with no version yet are skipped by `deploy.sh`; add them and redeploy:
```bash
printf '%s' 'sk_live_…' | gcloud secrets versions add SC_STRIPE_SECRET_KEY --data-file=-
printf '%s' 'whsec_…'   | gcloud secrets versions add SC_STRIPE_WEBHOOK_SECRET --data-file=-
printf '%s' 'whsec_…'   | gcloud secrets versions add SC_STRIPE_CONNECT_WEBHOOK_SECRET --data-file=-
printf '%s' '…'         | gcloud secrets versions add SC_GITHUB_CLIENT_SECRET --data-file=-
printf '%s' 'sk-ant-…'  | gcloud secrets versions add SC_COUNCIL_MODEL_KEY --data-file=-   # optional
```

## Data migration (done once)
`pg_dump` runs *inside* the Fly DB machine (its operator password never leaves it),
the dump is fetched over `fly ssh sftp`, and restored through `cloud-sql-proxy` with a
temporary public IP that is removed right after. See `infra/gcp/migrate-db.sh`.

## Cutover
1. Deploy to Cloud Run; smoke `$(URL)/healthz`, `/v1/manifest-key`, `/v1/council/roster`.
2. Point `api.ayni-ai.com` (Cloudflare DNS) at the load balancer IP; providers reconnect
   automatically (the daemon has a backoff loop). Node verify key changes: the KMS public
   key is served at `/v1/manifest-key` — providers on `SC_REQUIRE_MANIFEST=1` must switch
   `SC_MANIFEST_VERIFY_KEY` to `ayni-coordinator-kms-v1:<new key>`.
3. Stripe + GitHub OAuth callback URLs are unchanged (same hostname).
4. Scale Fly to zero after 24 h clean; destroy after the first weekly backup lands.

## Observability
Uptime check on `/healthz` with an alert policy; Cloud Run request/latency/5xx dashboard;
Data Access audit logs enabled for Secret Manager + KMS + Cloud SQL.
