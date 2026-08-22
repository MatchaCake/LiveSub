#!/bin/bash
# Deploy livesub to the VMware VM (christian-lee@192.168.1.144).
#
# The binary needs CGO (mattn/go-sqlite3), so we don't cross-compile from
# macOS: push the current branch to GitHub, pull it on the VM, and build
# there with the VM's own toolchain (/usr/local/go, go.mod may auto-fetch a
# newer toolchain on first build). Config, users.db and logs on the VM are
# never touched. The service is systemd-managed (livesub.service,
# Restart=always) and christian-lee has passwordless sudo for systemctl.
set -euo pipefail

HOST=christian-lee@192.168.1.144
REMOTE_DIR=/home/christian-lee/Projects/livesub
BRANCH="$(git -C "$(dirname "$0")" branch --show-current)"

cd "$(dirname "$0")"

if [ -n "$(git status --porcelain)" ]; then
  echo "error: uncommitted changes — commit (and push) before deploying" >&2
  exit 1
fi
git push origin "$BRANCH"

ssh "$HOST" "set -euo pipefail
  export PATH=\$PATH:/usr/local/go/bin
  cd $REMOTE_DIR
  git fetch origin
  git checkout $BRANCH
  git reset --hard origin/$BRANCH
  go build -o livesub.new ./cmd/livesub
  cp livesub livesub.bak.\$(date +%Y%m%d-%H%M%S)
  mv livesub.new livesub
  sudo -n systemctl restart livesub
  sleep 3
  systemctl is-active livesub
"

echo "deployed $BRANCH; panel: http://192.168.1.144:8899"
