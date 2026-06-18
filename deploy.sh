#!/bin/bash
# Deploy x402 Research Gateway to Hetzner VPS
# Usage: ./deploy.sh <server-ip>
#
# Prerequisites:
#   - SSH access to server (ssh root@<ip>)
#   - Docker + Docker Compose on server
#   - DNS: research.agfarms.dev → <server-ip>

set -e

SERVER=${1:?"Usage: ./deploy.sh <server-ip>"}
REMOTE="root@${SERVER}"

echo "=== Deploying to ${SERVER} ==="

# 1. Ensure Docker is on the server
ssh $REMOTE "which docker || (curl -fsSL https://get.docker.com | sh)"

# 2. Create deployment directory
ssh $REMOTE "mkdir -p /opt/x402-research"

# 3. Sync files
echo "Syncing gateway..."
rsync -avz --exclude='.git' --exclude='bin/' --exclude='.env' \
  /home/gian/freelance/x402-research-gateway/ $REMOTE:/opt/x402-research/gateway/

echo "Syncing Kruse search..."
rsync -avz --exclude='.git' --exclude='__pycache__' \
  /home/gian/jackkruse/ $REMOTE:/opt/x402-research/kruse/

# 4. Copy production env
scp /home/gian/freelance/x402-research-gateway/.env.prod $REMOTE:/opt/x402-research/gateway/.env

# 5. Build and start
ssh $REMOTE "cd /opt/x402-research/gateway && \
  docker compose -f docker-compose.prod.yml --env-file .env up -d --build"

echo ""
echo "=== Deployed! ==="
echo "Gateway: https://research.agfarms.dev/health"
echo "Kruse:   https://research.agfarms.dev/research/kruse/search?q=mitochondria"
echo ""
echo "Don't forget: point research.agfarms.dev DNS to ${SERVER}"
