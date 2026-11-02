#!/bin/bash
# Restrict the unprivileged SOCKS proxy to the WARP tunnel. The privileged WARP
# daemon can still establish its tunnel on the physical interface. Reply packets
# for connections initiated by the application are allowed back to the client.
set -eu

warp_uid="$(id -u)"
sudo nft delete table inet sub2api_warp_guard 2>/dev/null || true
sudo nft add table inet sub2api_warp_guard
# Docker redirects its embedded resolver before the regular filter priority.
# Block loopback port 53 before that redirect; the dedicated bridge uses 1053.
sudo nft add chain inet sub2api_warp_guard dns_output '{ type filter hook output priority -150; policy accept; }'
sudo nft add rule inet sub2api_warp_guard dns_output meta skuid "$warp_uid" ip daddr 127.0.0.0/8 udp dport 53 reject
sudo nft add rule inet sub2api_warp_guard dns_output meta skuid "$warp_uid" ip daddr 127.0.0.0/8 tcp dport 53 reject
sudo nft add chain inet sub2api_warp_guard output '{ type filter hook output priority -5; policy accept; }'
sudo nft add rule inet sub2api_warp_guard output meta skuid "$warp_uid" ct direction reply accept
# Docker's embedded resolver can send DNS outside the tunnel even when WARP is
# disconnected. The proxy must use the local DNS-over-HTTPS bridge instead.
sudo nft add rule inet sub2api_warp_guard output meta skuid "$warp_uid" ip daddr 127.0.0.11 reject
sudo nft add rule inet sub2api_warp_guard output meta skuid "$warp_uid" oifname lo accept
sudo nft add rule inet sub2api_warp_guard output meta skuid "$warp_uid" oifname CloudflareWARP accept
sudo nft add rule inet sub2api_warp_guard output meta skuid "$warp_uid" reject

python3 /etc/sub2api-warp-dns.py &
# This is intentionally not configurable: a system resolver would bypass the
# DNS bridge. Numeric DoH destinations in the bridge avoid recursive lookup.
export GOST_ARGS='-L=:1080?dns=127.0.0.1:1053'
exec /entrypoint.sh "$@"
