#!/bin/sh
# Egress allowlist for sr sandbox (ADR-0012). Requires NET_ADMIN.
# Resolves $SR_ALLOWLIST domains to IPs, allows only those (plus loopback,
# established connections, and DNS), then default-drops the rest and execs "$@".
# If iptables is unavailable it warns and runs without a firewall rather than
# trapping the agent offline.
set -e

if ! iptables -L >/dev/null 2>&1; then
    echo "sr sandbox: iptables unavailable — running WITHOUT egress firewall" >&2
    exec "$@"
fi

# Allow rules first (policy stays ACCEPT so DNS resolution below works).
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

ipset create sr_allow hash:ip 2>/dev/null || ipset flush sr_allow
OLDIFS=$IFS
IFS=,
for domain in $SR_ALLOWLIST; do
    [ -n "$domain" ] || continue
    for ip in $(getent ahostsv4 "$domain" 2>/dev/null | awk '{print $1}' | sort -u); do
        ipset add sr_allow "$ip" 2>/dev/null || true
    done
done
IFS=$OLDIFS

iptables -A OUTPUT -m set --match-set sr_allow dst -j ACCEPT

# Everything not explicitly allowed is now dropped.
iptables -P OUTPUT DROP

exec "$@"
