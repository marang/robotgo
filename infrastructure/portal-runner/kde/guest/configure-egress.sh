#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -ne 0 || $# -ne 0 ]]; then
  echo "invalid protected runner egress invocation" >&2
  exit 1
fi

nft delete table inet robotgo_runner >/dev/null 2>&1 || true
nft -f - <<'EOF'
table inet robotgo_runner {
  chain output {
    type filter hook output priority 0; policy drop;
    ct state established,related accept
    oifname "lo" accept
    ip daddr 10.0.2.2 tcp sport 22 accept
    ip daddr 10.0.2.2 tcp dport 3128 accept
    ip daddr 10.0.2.2 udp sport 68 udp dport 67 accept
    reject
  }
}
EOF
