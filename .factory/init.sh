#!/usr/bin/env sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE_CONFIG="${PICOCLAW_VALIDATION_SOURCE_CONFIG:-$ROOT/config/config.json}"
TEST_VAULT="${PICOCLAW_MUNINN_TEST_VAULT:-picoclaw-transparent-layer-test}"
MCP_ENDPOINT="${PICOCLAW_MUNINN_MCP_ENDPOINT:-http://127.0.0.1:8750}"
REST_ENDPOINT="${PICOCLAW_MUNINN_REST_ENDPOINT:-http://127.0.0.1:8475}"
FRONTDOOR_DIR="$ROOT/../claweb/access/frontdoor"

resolve_bin() {
  name="$1"
  candidate=""
  candidate="$(command -v "$name" 2>/dev/null || true)"
  if [ -z "$candidate" ] && command -v where.exe >/dev/null 2>&1; then
    candidate="$(where.exe "$name" 2>/dev/null | tr -d '\r' | head -n 1)"
  fi
  if [ -n "$candidate" ] && command -v cygpath >/dev/null 2>&1; then
    case "$candidate" in
      [A-Za-z]:\\*|[A-Za-z]:/*)
        candidate="$(cygpath -u "$candidate" 2>/dev/null || printf '%s' "$candidate")"
        ;;
    esac
  fi
  printf '%s' "$candidate"
}

GO_BIN="$(resolve_bin go)"
NODE_BIN="$(resolve_bin node)"
NPM_BIN="$(resolve_bin npm.cmd)"
[ -n "$NPM_BIN" ] || NPM_BIN="$(resolve_bin npm)"

cd "$ROOT"

[ -n "$GO_BIN" ] || { echo "go is required" >&2; exit 1; }
[ -n "$NODE_BIN" ] || { echo "node is required" >&2; exit 1; }
[ -n "$NPM_BIN" ] || { echo "npm is required" >&2; exit 1; }

[ -f "$SOURCE_CONFIG" ] || { echo "missing source config: $SOURCE_CONFIG" >&2; exit 1; }
[ -d "$FRONTDOOR_DIR" ] || { echo "missing claweb frontdoor dir: $FRONTDOOR_DIR" >&2; exit 1; }

mkdir -p "$ROOT/tmp"

"$GO_BIN" mod download
"$NPM_BIN" --prefix "$FRONTDOOR_DIR" install

printf '%s' 'claweb-dryrun-20260318' > "$ROOT/tmp/claweb.token"

export PICOCLAW_INIT_ROOT="$ROOT"
export PICOCLAW_INIT_SOURCE_CONFIG="$SOURCE_CONFIG"
export PICOCLAW_INIT_TEST_VAULT="$TEST_VAULT"
export PICOCLAW_INIT_MCP_ENDPOINT="$MCP_ENDPOINT"
export PICOCLAW_INIT_REST_ENDPOINT="$REST_ENDPOINT"

"$NODE_BIN" <<'NODE'
const fs = require('fs');
const path = require('path');

const root = process.env.PICOCLAW_INIT_ROOT;
const sourceConfigPath = process.env.PICOCLAW_INIT_SOURCE_CONFIG;
const testVault = process.env.PICOCLAW_INIT_TEST_VAULT;
const mcpEndpoint = process.env.PICOCLAW_INIT_MCP_ENDPOINT;
const restEndpoint = process.env.PICOCLAW_INIT_REST_ENDPOINT;

const source = JSON.parse(fs.readFileSync(sourceConfigPath, 'utf8'));
const out = {
  agents: { defaults: source?.agents?.defaults || {} },
  memory: {
    provider: 'muninndb',
    file: source?.memory?.file || { workspace: '~/.picoclaw/workspace' },
    muninndb: {
      mcp_endpoint: mcpEndpoint || source?.memory?.muninndb?.mcp_endpoint || 'http://127.0.0.1:8750',
      rest_endpoint: restEndpoint || source?.memory?.muninndb?.rest_endpoint || source?.memory?.muninndb?.mcp_endpoint || 'http://127.0.0.1:8475',
      vault: testVault,
      api_key: source?.memory?.muninndb?.api_key || '',
      timeout: source?.memory?.muninndb?.timeout || '30s'
    }
  },
  model_list: Array.isArray(source?.model_list) ? source.model_list : [],
  channels: {
    claweb: {
      enabled: true,
      listen_host: '127.0.0.1',
      listen_port: 18999,
      auth_token: '',
      auth_token_file: path.join(root, 'tmp', 'claweb.token').replace(/\\/g, '/'),
      allow_from: [],
      reasoning_channel_id: ''
    }
  },
  tools: source?.tools || {},
  heartbeat: source?.heartbeat || { enabled: true, interval: 30 },
  devices: source?.devices || { enabled: false, monitor_usb: true },
  voice: source?.voice || { echo_transcription: false },
  gateway: source?.gateway || { host: '127.0.0.1', port: 18790 }
};

fs.writeFileSync(path.join(root, 'tmp', 'claweb-dryrun.json'), JSON.stringify(out, null, 2));
NODE

echo "Prepared validation artifacts in $ROOT/tmp using test vault: $TEST_VAULT"
