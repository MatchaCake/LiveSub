#!/bin/bash
# Deploy livesub to the Synology NAS (docker, /volume1/docker/livesub). Replaces deploy-vmware.sh
# (VM 144 was retired; livesub moved to the NAS on 2026-11-23).
#
# Flow: refuse a dirty tree, push the branch, stream `git archive HEAD` to the NAS, build the image
# there with the repo Dockerfile (x86_64, CGO for mattn/go-sqlite3), retag compose to the new commit,
# `docker compose up -d`, check the container and the panel. configs/ (cookies, users.db,
# transcripts) and logs/ on the NAS are never touched. Needs `ssh nas-admin` (passwordless
# `sudo -n /usr/local/bin/docker`). Rollback: ./deploy-nas.sh --rollback <old-commit> (image must exist).
set -euo pipefail
NAS=nas-admin
DIR=/volume1/docker/livesub
DOCKER="sudo -n /usr/local/bin/docker"
cd "$(dirname "$0")"

if [ "${1:-}" = "--rollback" ]; then
  OLD="${2:?usage: deploy-nas.sh --rollback <commit>}"
  ssh "$NAS" "set -e; cd $DIR; $DOCKER image inspect livesub:$OLD >/dev/null
    sed -i.bak -E 's#^( +image: )livesub:[0-9a-f]+#\1livesub:$OLD#' compose.yaml
    $DOCKER compose up -d; sleep 5; $DOCKER ps --filter name=livesub --format '{{.Image}} {{.Status}}'"
  exit 0
fi

BRANCH="$(git branch --show-current)"
if [ -n "$(git status --porcelain)" ]; then
  echo "error: uncommitted changes — commit (and push) before deploying" >&2
  exit 1
fi
git push origin "$BRANCH"
SHA="$(git rev-parse --short HEAD)"

git archive --format=tar HEAD | ssh "$NAS" "set -e; rm -rf $DIR/src.new; mkdir -p $DIR/src.new; tar -C $DIR/src.new -xf -"
ssh "$NAS" "set -euo pipefail
  cd $DIR
  nice -n 10 $DOCKER build -t livesub:$SHA -t livesub:local src.new > build-$SHA.log 2>&1 || { tail -20 build-$SHA.log; exit 1; }
  rm -rf src.prev; [ -d src ] && mv src src.prev; mv src.new src
  sed -i.bak -E 's#^( +image: )livesub:[0-9a-f]+#\1livesub:$SHA#' compose.yaml
  $DOCKER compose up -d
  sleep 5
  $DOCKER ps --filter name=livesub --format '{{.Image}} {{.Status}}'
  curl -s -o /dev/null -w 'panel http %{http_code}\n' http://192.168.1.111:8899/
"
echo "deployed $BRANCH@$SHA; panel: http://192.168.1.111:8899 (LAN)"
