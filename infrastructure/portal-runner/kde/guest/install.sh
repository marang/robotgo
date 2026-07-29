#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
umask 077

if [[ $EUID -ne 0 ]]; then
  echo "KDE runner image installation requires root" >&2
  exit 1
fi
if [[ $# -ne 1 ]]; then
  echo "usage: install.sh <manifest.json>" >&2
  exit 1
fi

manifest=$1
test -f "$manifest"
test "$(jq -r '.schema_version' "$manifest")" = 1
test "$(jq -r '.lane' "$manifest")" = kde
test "$(jq -r '.repository' "$manifest")" = marang/robotgo

source /etc/os-release
test "$ID" = ubuntu
test "$VERSION_ID" = 24.04

apt_snapshot=$(jq -r '.apt_snapshot' "$manifest")
case "$apt_snapshot" in
  https://snapshot.ubuntu.com/ubuntu/*/) ;;
  *)
    echo "manifest does not contain a pinned Ubuntu snapshot" >&2
    exit 1
    ;;
esac

cat >/etc/apt/sources.list.d/ubuntu.sources <<EOF
Types: deb
URIs: $apt_snapshot
Suites: noble noble-updates noble-backports noble-security
Components: main universe restricted multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF
rm -f /etc/apt/sources.list

mapfile -t packages < <(jq -r '.packages[]' "$manifest")
test "${#packages[@]}" -gt 0
apt_options=(
  -o Acquire::Retries=5
  -o Acquire::http::Timeout=30
  -o Acquire::https::Timeout=30
  -o DPkg::Lock::Timeout=60
)
apt-get "${apt_options[@]}" update
apt-get "${apt_options[@]}" install -y --no-install-recommends "${packages[@]}"
kernel_release=$(jq -r '.vm.kernel_release' "$manifest")
test "$(uname -r)" = "$kernel_release"
test "$(dpkg-query -W -f='${db:Status-Status}' \
  "linux-modules-extra-$kernel_release")" = installed
test -f /usr/share/wayland-sessions/plasmawayland.desktop
test -x /usr/bin/kwin_wayland
test -x /usr/lib/x86_64-linux-gnu/libexec/xdg-desktop-portal-kde

if ! id robotgo >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash --user-group --uid 1100 robotgo
fi
test "$(id -u robotgo)" = 1100
passwd --lock robotgo
gpasswd --delete robotgo sudo >/dev/null 2>&1 || true

download_verified() {
  local url=$1
  local digest=$2
  local output=$3
  case "$url" in
    https://*) ;;
    *)
      echo "refusing non-HTTPS artifact URL" >&2
      exit 1
      ;;
  esac
  curl --fail --silent --show-error --location \
    --connect-timeout 15 --max-time 300 \
    --retry 5 --retry-delay 2 --retry-max-time 300 --retry-all-errors \
    --proto '=https' --tlsv1.2 \
    --output "$output" "$url"
  printf '%s  %s\n' "$digest" "$output" | sha256sum --check --status
}

go_archive=/var/tmp/robotgo-go.tar.gz
download_verified \
  "$(jq -r '.go.url' "$manifest")" \
  "$(jq -r '.go.sha256' "$manifest")" \
  "$go_archive"
rm -rf /usr/local/go
tar -C /usr/local -xzf "$go_archive"
rm -f "$go_archive"
ln -sfn /usr/local/go/bin/go /usr/local/bin/go
ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt
test "$(go env GOVERSION)" = "$(jq -r '.go.version' "$manifest")"

runner_archive=/var/tmp/robotgo-actions-runner.tar.gz
download_verified \
  "$(jq -r '.actions_runner.url' "$manifest")" \
  "$(jq -r '.actions_runner.sha256' "$manifest")" \
  "$runner_archive"
install -d -m 0755 -o robotgo -g robotgo /opt/actions-runner
tar -C /opt/actions-runner -xzf "$runner_archive"
rm -f "$runner_archive"
chown -R robotgo:robotgo /opt/actions-runner

install -d -m 0755 /usr/local/libexec
install -m 0755 "$(dirname "$0")/job-started.sh" \
  /usr/local/sbin/robotgo-runner-job-started
install -m 0755 "$(dirname "$0")/job-completed.sh" \
  /usr/local/sbin/robotgo-runner-job-completed
install -m 0755 "$(dirname "$0")/wait-session.sh" \
  /usr/local/libexec/robotgo-runner-wait-session
install -m 0755 "$(dirname "$0")/locate-screencast.sh" \
  /usr/local/libexec/robotgo-runner-locate-screencast
install -m 0755 "$(dirname "$0")/report-screencast-geometry.py" \
  /usr/local/libexec/robotgo-runner-report-screencast-geometry
install -d -m 0755 /usr/local/share/robotgo
install -m 0444 "$(dirname "$0")/report-screencast-geometry.js" \
  /usr/local/share/robotgo/report-screencast-geometry.js
install -m 0755 "$(dirname "$0")/configure-egress.sh" \
  /usr/local/sbin/robotgo-runner-configure-egress
install -m 0755 "$(dirname "$0")/register.sh" \
  /usr/local/sbin/robotgo-runner-register
cat >/usr/local/libexec/robotgo-runner-job-started-hook.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${GITHUB_EVENT_NAME:-}" in
  pull_request)
    expected_commit=$(jq -er '.pull_request.head.sha' "$GITHUB_EVENT_PATH")
    ;;
  push|workflow_dispatch)
    expected_commit=${GITHUB_SHA:-}
    ;;
  *)
    echo "unsupported protected runner event" >&2
    exit 1
    ;;
esac

exec sudo -n /usr/local/sbin/robotgo-runner-job-started \
  "$expected_commit" \
  "${GITHUB_RUN_ID:-}" \
  "${GITHUB_RUN_ATTEMPT:-}" \
  "${GITHUB_WORKFLOW:-}"
EOF
chmod 0755 /usr/local/libexec/robotgo-runner-job-started-hook.sh

cat >/usr/local/libexec/robotgo-runner-job-completed-hook.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exec sudo -n /usr/local/sbin/robotgo-runner-job-completed
EOF
chmod 0755 /usr/local/libexec/robotgo-runner-job-completed-hook.sh

cat >/etc/sudoers.d/robotgo-runner-hooks <<'EOF'
robotgo ALL=(root) NOPASSWD: /usr/local/sbin/robotgo-runner-job-started
robotgo ALL=(root) NOPASSWD: /usr/local/sbin/robotgo-runner-job-completed
EOF
chmod 0440 /etc/sudoers.d/robotgo-runner-hooks
visudo --check --file=/etc/sudoers.d/robotgo-runner-hooks

cat >/etc/systemd/system/robotgo-runner.service <<'EOF'
[Unit]
Description=Ephemeral RobotGo protected KDE portal runner
After=sddm.service network-online.target robotgo-runner-egress.service
Wants=network-online.target
Requires=robotgo-runner-egress.service
ConditionPathExists=/opt/actions-runner/.runner

[Service]
Type=simple
User=robotgo
Group=robotgo
WorkingDirectory=/opt/actions-runner
Environment=HOME=/home/robotgo
Environment=XDG_RUNTIME_DIR=/run/user/1100
Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus
Environment=WAYLAND_DISPLAY=wayland-0
Environment=XDG_CURRENT_DESKTOP=KDE
Environment=XDG_SESSION_DESKTOP=plasmawayland
Environment=XDG_SESSION_TYPE=wayland
Environment=DISPLAY=:0
Environment=ROBOTGO_COMPOSITOR_OUTPUT_COUNT=1
Environment=ROBOTGO_COMPOSITOR_OPERATOR_READY_FILE=/run/robotgo-evidence/operator-ready
Environment=ACTIONS_RUNNER_HOOK_JOB_STARTED=/usr/local/libexec/robotgo-runner-job-started-hook.sh
Environment=ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/usr/local/libexec/robotgo-runner-job-completed-hook.sh
Environment=HTTP_PROXY=http://10.0.2.2:3128
Environment=HTTPS_PROXY=http://10.0.2.2:3128
Environment=http_proxy=http://10.0.2.2:3128
Environment=https_proxy=http://10.0.2.2:3128
Environment=NO_PROXY=localhost,127.0.0.1
Environment=no_proxy=localhost,127.0.0.1
ExecStartPre=/usr/local/libexec/robotgo-runner-wait-session
ExecStart=/opt/actions-runner/run.sh
ExecStopPost=+/usr/local/sbin/robotgo-runner-job-completed
ExecStopPost=+/usr/bin/systemctl poweroff --no-wall
TimeoutStartSec=150
TimeoutStopSec=30
KillMode=control-group
Restart=no

[Install]
WantedBy=multi-user.target
EOF

cat >/etc/systemd/system/robotgo-runner-egress.service <<'EOF'
[Unit]
Description=Fail-closed RobotGo runner egress boundary
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/robotgo-runner-configure-egress
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

install -d -m 0755 /etc/sddm.conf.d
cat >/etc/sddm.conf.d/10-robotgo.conf <<'EOF'
[Autologin]
Relogin=true
Session=plasmawayland.desktop
User=robotgo

[General]
HaltCommand=/usr/bin/systemctl poweroff
RebootCommand=/usr/bin/systemctl reboot
EOF
chmod 0644 /etc/sddm.conf.d/10-robotgo.conf

install -d -m 0700 -o robotgo -g robotgo /home/robotgo/.config
cat >/home/robotgo/.config/kscreenlockerrc <<'EOF'
[Daemon]
Autolock=false
LockOnResume=false
Timeout=0
EOF
cat >/home/robotgo/.config/powermanagementprofilesrc <<'EOF'
[AC]
icon=preferences-system-power-management

[AC][DPMSControl]
idleTime=0

[AC][SuspendSession]
idleTime=0
suspendThenHibernate=false
suspendType=0
EOF
chown robotgo:robotgo \
  /home/robotgo/.config/kscreenlockerrc \
  /home/robotgo/.config/powermanagementprofilesrc
chmod 0600 \
  /home/robotgo/.config/kscreenlockerrc \
  /home/robotgo/.config/powermanagementprofilesrc

systemctl set-default graphical.target
systemctl enable sddm
systemctl enable robotgo-runner-egress.service

cat >/etc/ssh/sshd_config.d/90-robotgo-runner.conf <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PermitRootLogin prohibit-password
AllowUsers root
EOF
systemctl enable ssh

cat >/etc/sysctl.d/90-robotgo-runner.conf <<'EOF'
net.ipv6.conf.all.disable_ipv6=1
net.ipv6.conf.default.disable_ipv6=1
EOF

install -d -m 0700 /var/lib/robotgo-runner
cp "$manifest" /var/lib/robotgo-runner/manifest.json
chmod 0600 /var/lib/robotgo-runner/manifest.json
touch /var/lib/robotgo-runner/image-ready
chmod 0600 /var/lib/robotgo-runner/image-ready

apt-get clean
rm -rf /var/lib/apt/lists/*
rm -rf /root/.ssh /home/ubuntu/.ssh
rm -f /etc/ssh/ssh_host_*
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id
cloud-init clean --logs --seed
