#!/usr/bin/env bash
set -Eeuo pipefail

SERVER="${SERVER:-root@188.166.41.183}"
REMOTE_DIR="${REMOTE_DIR:-/root/msuparser}"
SERVICE="${SERVICE:-msuparser-bot}"

if ! command -v rsync >/dev/null 2>&1; then
  echo "rsync is required"
  exit 1
fi

if ! command -v ssh >/dev/null 2>&1; then
  echo "ssh is required"
  exit 1
fi

echo "==> Deploy target: ${SERVER}:${REMOTE_DIR}"

echo "==> Checking remote bot processes"
ssh "${SERVER}" "REMOTE_DIR='${REMOTE_DIR}' bash -s" <<'REMOTE_CHECK'
set -Eeuo pipefail

legacy_processes="$(ps -eo pid=,ppid=,args= | awk '/\/root\/code\/projects\/msuparser\/bot/ && !/awk/ {print}')"
if [ -n "${legacy_processes}" ]; then
  echo "Legacy bot process is still running. Stop it before deploy:"
  echo "${legacy_processes}"
  exit 1
fi

new_processes="$(ps -eo pid=,ppid=,args= | awk -v remote_dir="${REMOTE_DIR}" 'index($0, remote_dir "/main") && !/awk/ {print}')"
new_count="$(printf '%s\n' "${new_processes}" | sed '/^$/d' | wc -l)"
if [ "${new_count}" -gt 1 ]; then
  echo "More than one new bot process is running. Refusing to deploy:"
  echo "${new_processes}"
  exit 1
fi

if [ "${new_count}" -eq 1 ]; then
  echo "Remote bot process OK:"
  echo "${new_processes}"
else
  echo "No running ${REMOTE_DIR}/main process found; deploy will start it via systemd."
fi
REMOTE_CHECK

echo "==> Local checks"
check_dir="$(mktemp -d)"
trap 'rm -rf "${check_dir}"' EXIT
GOCACHE="${GOCACHE:-${check_dir}/gocache}" go build -o "${check_dir}/main" main.go parser.go gifs.go changes.go
GOCACHE="${GOCACHE:-${check_dir}/gocache}" go build -o "${check_dir}/test_parser" test_parser.go parser.go

echo "==> Preparing remote directory"
ssh "${SERVER}" "mkdir -p '${REMOTE_DIR}' '${REMOTE_DIR}/backups'"

echo "==> Uploading project files"
rsync -az \
  --exclude='.git/' \
  --exclude='.codex/' \
  --exclude='logs/' \
  --exclude='*.log' \
  --exclude='config.json' \
  --exclude='config.py' \
  --exclude='schedule.json' \
  --exclude='schedule_meta.json' \
  --exclude='gifs.json' \
  --exclude='*_test.go' \
  --exclude='main' \
  --exclude='test_parser' \
  ./ "${SERVER}:${REMOTE_DIR}/"

echo "==> Building and restarting on server"
ssh "${SERVER}" "cd '${REMOTE_DIR}' && \
  ts=\$(date +%Y%m%d-%H%M%S) && \
  backup_dir='${REMOTE_DIR}/backups/deploy-'\$ts && \
  mkdir -p \"\$backup_dir\" && \
  for f in main test_parser main.go parser.go gifs.go changes.go test_parser.go config.json schedule.json schedule_meta.json gifs.json; do \
    if [ -e \"\$f\" ]; then cp -a \"\$f\" \"\$backup_dir/\"; fi; \
  done && \
  go build -o main.new main.go parser.go gifs.go changes.go && \
  go build -o test_parser.new test_parser.go parser.go && \
  install -m 755 main.new main && \
  install -m 755 test_parser.new test_parser && \
  rm -f main.new test_parser.new && \
  systemctl restart '${SERVICE}' && \
  sleep 2 && \
  systemctl is-active --quiet '${SERVICE}' && \
  systemctl --no-pager --lines=0 status '${SERVICE}'"

echo "==> Deploy completed"
