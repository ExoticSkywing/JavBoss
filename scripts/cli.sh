#!/usr/bin/env bash
set -euo pipefail

trap 'printf "\e[?25h"' EXIT

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_ROOT="$SCRIPT_DIR/cli"
CLI_BIN="$CLI_ROOT/build/javboss-cli.cjs"
NEED_BUILD=0

# The Telegram/CD2 integration is configured in the sibling Bot project.  Load
# that file automatically for the development command so the normal
# `./scripts/cli.sh dev both` workflow also starts JavBoss with the same
# integration credentials.  An explicit JAVBOSS_ENV_FILE takes precedence;
# set JAVBOSS_AUTO_ENV=0 to opt out (for isolated backend tests).
if [[ "${1:-}" == "dev" && "${JAVBOSS_AUTO_ENV:-1}" != "0" ]]; then
  INTEGRATION_ENV_FILE="${JAVBOSS_ENV_FILE:-/root/data/docker_data/cd2_magnet_tgbot/.env}"
  if [[ -f "$INTEGRATION_ENV_FILE" ]]; then
    set -a
    # This is an operator-owned dotenv file, not untrusted input.  Keeping the
    # source here means quoted values such as CLEAN_CRON remain valid.
    # shellcheck disable=SC1090
    source "$INTEGRATION_ENV_FILE"
    set +a

    : "${GATEWAY_PORT:=18081}"
    export JAVBOSS_CLOUD_DOWNLOAD_URL="${JAVBOSS_CLOUD_DOWNLOAD_URL:-http://127.0.0.1:${GATEWAY_PORT}/v1/javboss/download-batches}"
    export JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL="${JAVBOSS_CLOUD_DOWNLOAD_REVIEW_URL:-http://127.0.0.1:${GATEWAY_PORT}/v1/javboss/download-attempts/{attempt_id}/review}"
    export JAVBOSS_CLOUD_DOWNLOAD_REVIEW_BATCH_URL="${JAVBOSS_CLOUD_DOWNLOAD_REVIEW_BATCH_URL:-http://127.0.0.1:${GATEWAY_PORT}/v1/javboss/download-attempts/review-batch}"
    export JAVBOSS_CLOUD_DOWNLOAD_TOKEN="${JAVBOSS_CLOUD_DOWNLOAD_TOKEN:-${JAVBOSS_GATEWAY_TOKEN:-}}"
    export JAVBOSS_CLOUD_DOWNLOAD_CALLBACK_TOKEN="${JAVBOSS_CLOUD_DOWNLOAD_CALLBACK_TOKEN:-${JAVBOSS_CALLBACK_TOKEN:-}}"
    echo "[env] 已加载 CloudDrive2/JavBoss 联调配置：$INTEGRATION_ENV_FILE"
  fi
fi

if [[ ! -f "$CLI_BIN" ]]; then
  NEED_BUILD=1
else
  if find "$CLI_ROOT" -type f \( -name "*.mjs" -o -name "*.js" -o -name "*.json" \) \
    ! -path "$CLI_ROOT/node_modules/*" ! -path "$CLI_ROOT/build/*" \
    -newer "$CLI_BIN" -print -quit | grep -q .; then
    NEED_BUILD=1
  fi
fi

if [[ "$NEED_BUILD" == "1" ]]; then
  echo "bundled CLI missing or stale; building..." >&2
  pushd "$CLI_ROOT" >/dev/null
  if [[ ! -d node_modules || package.json -nt node_modules || package-lock.json -nt node_modules ]]; then
    if ! command -v npm >/dev/null 2>&1; then
      echo "npm not found; please install Node.js/npm to build CLI" >&2
      popd >/dev/null
      exit 1
    fi
    npm ci
  fi
  npm run build
  popd >/dev/null
fi

node "$CLI_BIN" "$@"
