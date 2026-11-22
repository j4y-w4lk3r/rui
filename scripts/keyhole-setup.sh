#!/bin/bash
# keyhole-setup.sh: a new OpenBao token for rui, kept ONLY in keyhole (entry openbao-rui).
#   - policy rui (read secret/rui/* only), periodic: rui renews it on every start, so it lives as
#     long as rui is used (the server's max TTL caps the period); nothing is written to disk
#   - made over ssh on the OpenBao host ($RUI_BAO_SSH, with the bao CLI and ~/.config/j4y/openbao.addr)
#     with the root token from keyhole entry openbao-root (touch), saved with keyhole add (touch + PIN)
#   - then read back through keyhole and checked against secret/rui/<first site in settings.json, or $RUI_CHECK_SITE> (HTTP code only)
# Vault and identity: keyhole_vault / keyhole_identity in ~/.config/rui/settings.json (as rui reads them).
#   RUI_BAO_SSH=host bash scripts/keyhole-setup.sh
set -euo pipefail
die() { echo "!! $*" >&2; exit 1; }
[ -n "${RUI_BAO_SSH:-}" ] || die "set RUI_BAO_SSH to the ssh host that runs the bao CLI"
S=${XDG_CONFIG_HOME:-$HOME/.config}/rui/settings.json
read -r V ID SITE < <(python3 -I -c 'import json,os,sys
s=json.load(open(sys.argv[1])); e=lambda p: os.path.expanduser(p or "")
ids=[x.get("id","") for x in s.get("sites",[])]+list(s.get("gateway_macs",{}))
print(e(s.get("keyhole_vault")), e(s.get("keyhole_identity")), (ids+["-"])[0].lower())' "$S" 2>/dev/null) || true
SITE=${RUI_CHECK_SITE:-$SITE}; [ -n "$SITE" ] && [ "$SITE" != - ] || die "no site in $S (sites / gateway_macs) to check the token against"
[ -r "${V:-}" ] && [ -r "${ID:-}" ] || die "keyhole_vault / keyhole_identity in $S missing or not readable (stick in?)"
if keyhole ask openbao-rui </dev/null 2>/dev/null | grep -q .; then die "keyhole openbao-rui already exists: nothing to do"; fi
BAO='PATH=/usr/bin:/bin; read -r BAO_TOKEN; export BAO_TOKEN; export BAO_ADDR=$(cat ~/.config/j4y/openbao.addr);'
echo "  OpenBao root token: touch the key"
root=$(keyhole get "$V" -i "$ID" openbao-root </dev/null 2>/dev/null | tr -d '\r\n')
[ ${#root} -ge 20 ] || die "no openbao-root (touch missed?)"
tok=$(printf '%s\n' "$root" | ssh -o BatchMode=yes "$RUI_BAO_SSH" "$BAO bao token create -orphan -policy=rui -period=8760h -display-name=rui-keyhole -field=token" | tr -d '\r\n')
unset root
[ ${#tok} -ge 20 ] || die "token create failed"
echo "  keyhole add openbao-rui: touch the key, then the PIN"
printf %s "$tok" | keyhole add "$V" -i "$ID" openbao-rui --username rui --password-stdin || die "keyhole add failed (token not saved; run again)"
unset tok
back=$(keyhole get "$V" -i "$ID" openbao-rui </dev/null 2>/dev/null | tr -d '\r\n')   # touch
[ ${#back} -ge 20 ] || die "could not read openbao-rui back from keyhole"
addr=$(ssh -o BatchMode=yes "$RUI_BAO_SSH" 'cat ~/.config/j4y/openbao.addr')
code=$(curl -s -o /dev/null -w '%{http_code}' -H @<(printf 'X-Vault-Token: %s\n' "$back") "$addr/v1/secret/data/rui/$SITE")
unset back
[ "$code" = 200 ] || die "the new token cannot read secret/rui/$SITE (HTTP $code)"
echo "  keyhole openbao-rui: saved; reads secret/rui/$SITE (HTTP 200)"
