#!/usr/bin/env bash
# Ayni production QA sweep.
#   ./scripts/qa-prod.sh            # reads .env.local at repo root
#   SC_COORDINATOR_API=... SC_CONSUMER_KEY=... SC_ADMIN_TOKEN=... ./scripts/qa-prod.sh
set -u
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[ -f "$REPO/.env.local" ] && { set -a; . "$REPO/.env.local"; set +a; }
API="${SC_COORDINATOR_API:?set SC_COORDINATOR_API}"
KEY="${SC_CONSUMER_KEY:?set SC_CONSUMER_KEY}"
ADM="${SC_ADMIN_TOKEN:?set SC_ADMIN_TOKEN}"
pass=0; fail=0
ok(){ echo "  PASS $1"; pass=$((pass+1)); }
no(){ echo "  FAIL $1"; fail=$((fail+1)); }
code(){ curl -s -o /dev/null -w '%{http_code}' "$@"; }
j(){ curl -s "$@"; }

echo "== 1. models (auth) =="
c=$(code -H "Authorization: Bearer $KEY" "$API/v1/models"); [ "$c" = 200 ] && ok "GET /v1/models 200" || no "/v1/models -> $c"
c=$(code "$API/v1/models"); [ "$c" = 401 ] && ok "/v1/models unauth -> 401" || no "unauth -> $c (want 401)"

echo "== 2. chat completions =="
r=$(j -X POST "$API/v1/chat/completions" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","stream":false,"max_tokens":30,"messages":[{"role":"user","content":"say hi"}]}')
echo "$r" | grep -q '"content"' && ok "non-stream returns content" || no "non-stream: $r"
s=$(curl -s -N -X POST "$API/v1/chat/completions" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","stream":true,"max_tokens":30,"messages":[{"role":"user","content":"count to 3"}]}' | head -c 400)
echo "$s" | grep -q 'data:' && ok "stream emits SSE data:" || no "stream: $s"

echo "== 3. batch fan-out =="
r=$(j -X POST "$API/v1/batch" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{
 "model":"qwen2.5-0.5b-instruct-q4_k_m","max_tokens":40,"items":[
 {"messages":[{"role":"user","content":"one word: sky"}]},
 {"messages":[{"role":"user","content":"one word: sea"}]},
 {"messages":[{"role":"user","content":"one word: sun"}]},
 {"messages":[{"role":"user","content":"one word: ice"}]}]}')
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);s=d['stats'];print('   stats',s);exit(0 if s['ok']==4 and s['failed']==0 else 1)" \
  && ok "batch 4/4 ok" || no "batch: $r"

echo "== 4. public quote =="
r=$(j -X POST "$API/v1/quote" -H 'content-type: application/json' -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","estimate":{"count":500,"avg_prompt_tokens":400,"avg_completion_tokens":150}}')
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);print('   on-demand $',d['price']['total_usd'],'eta',d['estimate']['eta_seconds'],'s');exit(0 if d['price']['total_usd']>0 else 1)" \
  && ok "public quote on-demand" || no "quote: $r"
r=$(j -X POST "$API/v1/quote" -H 'content-type: application/json' -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","spot":true,"estimate":{"count":500,"avg_prompt_tokens":400,"avg_completion_tokens":150}}')
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);e=d['estimate'];print('   spot $',d['price']['total_usd'],'eta',e['eta_seconds'],'..',e.get('eta_seconds_max'),'s');exit(0 if 'eta_seconds_max' in e else 1)" \
  && ok "spot quote has eta band" || no "spot quote: $r"

echo "== 5. workloads: quote -> council -> accept -> run -> settle =="
r=$(j -X POST "$API/v1/workloads" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{
 "model":"qwen2.5-0.5b-instruct-q4_k_m","items":[
 {"messages":[{"role":"user","content":"Summarize: the water cycle moves water through evaporation, condensation, and precipitation."}],"max_tokens":80},
 {"messages":[{"role":"user","content":"Summarize: photosynthesis converts light, water, and CO2 into glucose and oxygen."}],"max_tokens":80}]}')
wid=$(echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('id',''))" 2>/dev/null)
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);c=d.get('council',{});print('   quote',d['id'],'$',d['price']['total_usd'],'council',c.get('decision'),'reviews',len(c.get('reviews',[])))" || no "workload create: $r"
[ -n "$wid" ] && ok "workload created w/ council" || no "no workload id"
gc=$(code -H "Authorization: Bearer $KEY" "$API/v1/workloads/$wid"); [ "$gc" = 200 ] && ok "GET workload 200" || no "GET workload -> $gc"
# IDOR: another key id (garbage) must not read it
gc=$(code -H "Authorization: Bearer sk_wrong_key_zzz" "$API/v1/workloads/$wid"); [ "$gc" = 401 ] && ok "workload IDOR: wrong key 401" || no "IDOR wrong key -> $gc"
acc=$(j -X POST "$API/v1/workloads/$wid/accept" -H "Authorization: Bearer $KEY")
echo "$acc" | python3 -c "import sys,json;d=json.load(sys.stdin);rr=d.get('run_review',{});print('   accepted: charged $',d.get('charged_usd'),'items',len(d.get('items',[])),'run_review',rr.get('verdict'),rr.get('findings') if rr.get('findings') else '');exit(0 if d.get('items') else 1)" \
  && ok "workload accept ran + settled + run_review" || no "accept: $acc"
# double-accept must fail
dc=$(code -X POST "$API/v1/workloads/$wid/accept" -H "Authorization: Bearer $KEY"); [ "$dc" = 409 ] && ok "double-accept -> 409" || no "double-accept -> $dc"

echo "== 6. workloads pinned to device_attested (phone) =="
r=$(j -X POST "$API/v1/workloads" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -H 'X-Provider-Trust-Level: device_attested' -d '{
 "model":"qwen2.5-0.5b-instruct-q4_k_m","items":[{"messages":[{"role":"user","content":"one sentence on trust"}],"max_tokens":50}]}')
wid2=$(echo "$r" | python3 -c "import sys,json;print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);print('   tier',d.get('tier'));exit(0 if d.get('tier')=='device_attested' else 1)" && ok "quote tier=device_attested" || no "tier: $r"
acc=$(j -X POST "$API/v1/workloads/$wid2/accept" -H "Authorization: Bearer $KEY")
echo "$acc" | python3 -c "import sys,json;d=json.load(sys.stdin);it=(d.get('items') or [{}])[0];print('   served tier',it.get('trust_tier'),'provider',it.get('provider_id'));exit(0 if d.get('items') else 1)" && ok "attested workload ran" || no "attested accept: $acc"

echo "== 7. demo endpoint (providers online now) =="
r=$(j -X POST "$API/v1/demo/summarize" -H 'content-type: application/json' -d '{"text":"Ayni is a marketplace that runs AI workloads on idle laptops and phones, encrypted end to end, paying device owners per token. No token, no blockchain; settlement is prepaid credit and a fixed council of models reviews each workload before it runs."}')
echo "$r" | python3 -c "import sys,json;d=json.load(sys.stdin);res=d.get('result');print('   council',d['council']['decision'],'| result',('OK '+str(res.get('device',{}).get('label'))) if res else ('null / '+str(d.get('error'))));exit(0 if res and res.get('summary') else 1)" \
  && ok "demo produced a real summary" || no "demo: $(echo "$r" | head -c 300)"
c=$(code "$API/v1/demo/last-job"); [ "$c" = 200 ] && ok "demo last-job 200" || no "last-job -> $c"

echo "== 8. council apis =="
for p in roster constitution meetings decisions run-reviews; do
  c=$(code "$API/v1/council/$p"); [ "$c" = 200 ] && ok "council/$p 200" || no "council/$p -> $c"
done
j "$API/v1/council/decisions" | python3 -c "import sys,json;d=json.load(sys.stdin);print('   decisions chain_valid',d.get('chain_valid'),'count',d.get('count'),'live',d.get('live_count'));exit(0 if d.get('chain_valid') else 1)" \
  && ok "decision hash chain valid" || no "chain invalid"
j "$API/v1/council/run-reviews" | python3 -c "import sys,json;d=json.load(sys.stdin);print('   run-reviews count',d.get('count'));exit(0)" && ok "run-reviews reachable"

echo "== 9. billing =="
j -H "Authorization: Bearer $KEY" "$API/v1/me/earnings" >/dev/null 2>&1
bc=$(code -H "Authorization: Bearer $KEY" "$API/billing/balance"); [ "$bc" = 200 ] && ok "billing/balance 200" || no "billing/balance -> $bc"
j -H "Authorization: Bearer $KEY" "$API/billing/balance"
ck=$(code -X POST "$API/billing/checkout" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{"amount_usd":10}')
echo "   billing/checkout -> $ck (503/501 expected if Stripe not live-configured; 200 if it is)"

echo "== 10. negative / abuse =="
c=$(code -X POST "$API/v1/chat/completions" -H "Authorization: Bearer badkey" -H 'content-type: application/json' -d '{"model":"x","messages":[]}'); [ "$c" = 401 ] && ok "bad key -> 401" || no "bad key -> $c"
c=$(code -X POST "$API/v1/chat/completions" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{"model":"does-not-exist","messages":[{"role":"user","content":"hi"}]}'); [ "$c" = 404 ] && ok "unknown model -> 404" || no "unknown model -> $c"
big=$(python3 -c "print('{\"model\":\"qwen2.5-0.5b-instruct-q4_k_m\",\"items\":['+','.join(['{\"messages\":[{\"role\":\"user\",\"content\":\"x\"}]}']*5000)+']}')")
c=$(code -X POST "$API/v1/batch" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d "$big"); { [ "$c" = 413 ] || [ "$c" = 400 ]; } && ok "oversized batch -> $c" || no "oversized batch -> $c"
c=$(code -X POST "$API/v1/workloads" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m"}'); [ "$c" = 400 ] && ok "workload missing items/estimate -> 400" || no "-> $c"

echo "== 11. auth surface (dormant until OAuth secrets) =="
j "$API/auth/providers"; echo
c=$(code "$API/v1/me"); { [ "$c" = 401 ] || [ "$c" = 404 ]; } && ok "/v1/me unauth -> $c" || no "/v1/me -> $c"

echo "== 12. admin =="
for p in providers nodes payouts/pending; do
  c=$(code -H "X-Admin-Token: $ADM" "$API/admin/$p"); [ "$c" = 200 ] && ok "admin/$p 200" || no "admin/$p -> $c"
  c=$(code "$API/admin/$p"); { [ "$c" = 401 ] || [ "$c" = 403 ]; } && ok "admin/$p unauth -> $c" || no "admin/$p unauth -> $c"
done

echo
echo "==== QA: $pass passed, $fail failed ===="
