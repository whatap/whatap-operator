#!/bin/bash
# New Linux guests only. Never print or source the secret configuration.
set +x
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C
unset TAR_OPTIONS
umask 077
trap 'printf "WhaTap bootstrap failed; inspect installer/service state locally.\n" >&2' ERR
fail() { printf 'WhaTap bootstrap refused an unsupported or conflicting guest state.\n' >&2; exit 1; }
agent_version='__WHATAP_AGENT_VERSION__'
source_config=/etc/whatap-kubevirt/whatap.conf
state=/var/lib/whatap-kubevirt
agent_dir=/usr/whatap/infra
agent_config=$agent_dir/conf/whatap.conf
# Reject symlinks, including ancestors, before any write to a reserved path.
safe_path() {
  local probe=$1
  while [[ -n "$probe" ]]; do
    [[ ! -L "$probe" ]] || fail
    probe=${probe%/*}
  done
}
[[ $(id -u) = 0 ]] || fail
safe_path "$state/success"
# Invalidate the previous run before any OS/config/download/service failure.
rm -f "$state/success"
for file in "$source_config" "$agent_config" "$state/input.conf" "$state/managed.conf" "$agent_dir/conf/VERSION" "$agent_dir/whatap_infrad" "$agent_dir/infra_agent_patch.sh" "$agent_dir/ChangeLog.txt" "$agent_dir/.install_prefix" /etc/whatap/infra.meta /etc/systemd/system/whatap-infra.service /etc/systemd/system/whatap-updater.service /etc/init.d/whatap-infra /etc/init.d/whatap-updater; do
  safe_path "$file"
done
[[ $(uname -s) = Linux && -d /run/systemd/system && -r /etc/os-release ]] || fail
. /etc/os-release
case "${ID:-}" in
  debian|ubuntu|rhel|centos|rocky|almalinux|ol|amzn) ;;
  *) fail ;;
esac
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) printf 'WhaTap bootstrap requires an amd64 or arm64 guest.\n' >&2; exit 1 ;;
esac
[[ "$agent_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail
for tool in systemctl curl tar; do
  command -v "$tool" >/dev/null || { printf 'WhaTap bootstrap requires systemctl, curl, tar and trusted CA certificates.\n' >&2; exit 1; }
done
# curl uses the guest's trusted CA store; never bypass certificate verification.
[[ -f "$source_config" && -s "$source_config" ]] || fail
# An ownership snapshot permits retries, but never takes over a foreign agent.
if [[ -f "$state/managed.conf" ]]; then
  cmp -s "$source_config" "$state/input.conf" || fail
  cmp -s "$agent_config" "$state/managed.conf" || fail
else
  [[ ! -e "$agent_dir" && ! -e "$agent_config" ]] || fail
fi
install -d -m 0700 "$state"
chown root:root "$state" "$source_config"
chmod 0600 "$source_config"
if [[ ! -f "$state/managed.conf" ]]; then
  install -m 0600 "$source_config" "$state/input.conf"
  install -m 0600 "$source_config" "$state/managed.conf"
  printf 'createdtime=%s000000000\n' "$(date +%s)" >> "$state/managed.conf"
  install -d -m 0755 "$agent_dir/conf"
  install -m 0600 "$state/managed.conf" "$agent_config"
fi
chown root:root "$agent_config" "$state/input.conf" "$state/managed.conf"
chmod 0600 "$agent_config"

# Every invocation downloads and runs the official installer. Ownership markers
# protect configuration, not an installation cache or a package database gate.
work=$(mktemp -d "$state/download.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
trap 'exit 1' HUP INT TERM
archive=$work/agent.tar.gz
if ! curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 10 --max-time 120 --retry 2 --retry-max-time 300 --output "$archive" "https://repo.whatap.io/infra/whatap-infra-${agent_version}-linux-${arch}.tar.gz" >/dev/null 2>&1; then
  printf 'WhaTap archive download failed; check HTTPS connectivity and trusted CA certificates.\n' >&2
  exit 1
fi
# List without path rewriting, then validate BEFORE extracting anything. Reject
# links/special files and ambiguous names (whitespace, escapes, dot components,
# duplicate entries). This is archive safety/version checking, NOT a signature.
tar -tzPf "$archive" > "$work/names" 2>/dev/null
tar -tvzPf "$archive" > "$work/types" 2>/dev/null
names=()
while IFS= read -r name; do
  [[ "$name" =~ ^whatap-infra(/[A-Za-z0-9_.-]+)*/?$ ]] || fail
  case "/$name/" in */../*|*/./*) fail ;; esac
  name=${name%/}
  for previous in ${names[@]+"${names[@]}"}; do [[ "$name" != "$previous" ]] || fail; done
  names+=("$name")
done < "$work/names"
count=0
while IFS= read -r entry; do
  case "$entry" in -*|d*) ;; *) fail ;; esac
  count=$((count + 1))
done < "$work/types"
[[ "$count" -gt 0 && "$count" = "${#names[@]}" ]] || fail
mkdir "$work/extracted"
tar -xzf "$archive" --no-same-owner --no-same-permissions -C "$work/extracted" >/dev/null 2>&1
release=$work/extracted/whatap-infra
[[ -f "$release/install.sh" && -f "$release/whatap_infrad" && -f "$release/conf/VERSION" ]] || fail
[[ $(< "$release/conf/VERSION") = "$agent_version" ]] || fail
# 2.9.25 supports --prefix, not --silent. It preserves whatap.conf and only
# registers services; no service mask is needed and no license enters argv.
# It writes systemd units with `cat >` and no chmod, so run it under the
# standard umask (units 0644; 0600 makes systemd warn on every reload). This
# shell keeps umask 077 and secret files keep their explicit 0600 modes.
(umask 022 && exec /bin/bash "$release/install.sh" --prefix "$agent_dir") >/dev/null 2>&1
safe_path "$agent_config"
safe_path "$agent_dir/conf/VERSION"
cmp -s "$agent_config" "$state/managed.conf" || fail
[[ -f "$agent_dir/conf/VERSION" && $(< "$agent_dir/conf/VERSION") = "$agent_version" ]] || fail
chown root:root "$agent_config"
chmod 0600 "$agent_config"
systemctl daemon-reload >/dev/null 2>&1
systemctl enable whatap-infra.service >/dev/null 2>&1
systemctl start whatap-infra.service >/dev/null 2>&1
systemctl is-active --quiet whatap-infra.service >/dev/null 2>&1
# This attests only installer completion and service start, NOT telemetry delivery.
printf 'install-and-service-ready\n' > "$state/success"
