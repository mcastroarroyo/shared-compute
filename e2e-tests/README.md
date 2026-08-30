# e2e-tests

`run.sh` boots the coordinator and one provider (mock backend), then exercises the full
path a real request takes:

1. build `coordinator` and `provider-daemon`
2. start the coordinator with throwaway keys on a high port
3. start the provider (`SC_BACKEND=mock`, model `mock-echo`), which registers over WSS
4. **streaming** `POST /v1/chat/completions` → assert reassembled SSE content
5. **non-streaming** `POST /v1/chat/completions` → assert `choices[0].message.content` + `usage`
6. assert `401` without an API key
7. assert neither process log contains the prompt or completion text

Every job in the test goes through the real envelope: coordinator generates a per-job
X25519 ephemeral keypair, seals the request with NaCl `crypto_box` to the provider's
registered key, the provider seals each token chunk back, and the coordinator decrypts and
relays as SSE.

## Run

```bash
./e2e-tests/run.sh
# keep the temp dir + logs:
SC_E2E_KEEP=1 ./e2e-tests/run.sh
# override the port:
SC_E2E_PORT=9100 ./e2e-tests/run.sh
```

## Next

- swap `SC_BACKEND=mock` for `llama` + a real GGUF once the llama.cpp backend lands
- add a cancellation case (client disconnects mid-stream → provider gets `cancel`)
- run against the deployed coordinator as a post-deploy smoke test (M2)
