# Always-on demo provider node. Pulls the prebuilt provider-daemon + model via
# the coordinator's installer and runs it. State (binary + GGUF) persists in a
# volume so restarts are instant.
FROM debian:12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/ayni
ENV AYNI_DIR=/opt/ayni/state

# SC_REGISTRATION_TOKEN comes from the environment (compose / -e). SC_API lets you
# point at a staging coordinator.
ENTRYPOINT ["bash","-lc","curl -fsSL ${SC_API:-https://api.ayni-ai.com}/install/provider.sh | bash"]
