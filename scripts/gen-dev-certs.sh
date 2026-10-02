#!/usr/bin/env bash
# Generate a local development CA + master/agent leaf certs for OMLS 0.2 mTLS.
# Dev / lab only. Do not reuse these certs outside a trusted LAN.
set -euo pipefail

OUT="${1:-./dev-certs}"
DAYS="${DAYS:-825}"
MASTER_CN="${MASTER_CN:-omls-master}"
AGENT_CN="${AGENT_CN:-omls-agent}"

mkdir -p "$OUT"
cd "$OUT"

if [[ -f ca.crt && -f master.crt && -f agent.crt ]]; then
  echo "certs already present in $OUT (delete to regenerate)"
  exit 0
fi

# CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days "$DAYS" \
  -subj "/O=OMLS Dev/CN=OMLS Dev CA" -out ca.crt

# Master (server + client so operators can call GetGraph)
openssl genrsa -out master.key 2048
openssl req -new -key master.key -subj "/O=OMLS Dev/CN=${MASTER_CN}" -out master.csr
cat > master.ext <<EOF
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth,clientAuth
subjectAltName=DNS:localhost,DNS:omls-master,IP:127.0.0.1,IP:::1
EOF
openssl x509 -req -in master.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out master.crt -days "$DAYS" -sha256 -extfile master.ext

# Agent (client)
openssl genrsa -out agent.key 2048
openssl req -new -key agent.key -subj "/O=OMLS Dev/CN=${AGENT_CN}" -out agent.csr
cat > agent.ext <<EOF
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
subjectAltName=DNS:localhost,DNS:omls-agent,IP:127.0.0.1,IP:::1
EOF
openssl x509 -req -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out agent.crt -days "$DAYS" -sha256 -extfile agent.ext

rm -f master.csr agent.csr master.ext agent.ext ca.srl
chmod 600 ca.key master.key agent.key
echo "wrote CA + master + agent certs under $(pwd)"
