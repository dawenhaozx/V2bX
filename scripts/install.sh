#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY=dawenhaozx/V2bX
INSTALL_DIR=/usr/local/V2bX
CONFIG_DIR=/etc/V2bX
SERVICE_FILE=/etc/systemd/system/V2bX.service
BIN_LINK=/usr/local/bin/V2bX
VERSION=
ARCH=
WORK_DIR=
STAGED_BINARY=
BACKUP_BINARY=
BINARY_REPLACED=false
HAD_BINARY=false
WAS_ACTIVE=false
CREATED_SERVICE=false
CREATED_LINK=false

usage() {
    cat <<'EOF'
Usage: bash install.sh [--version TAG] [--install-dir DIR] [--arch NAME]

Install the latest release from github.com/dawenhaozx/V2bX, or select a release
tag with --version (for example v1.0.0). Existing configuration and certificates
are preserved. A new installation is not enabled or started automatically.

Linux with systemd is required, together with curl, unzip and sha256sum.
--install-dir defaults to /usr/local/V2bX; updates may select an existing path.
--arch accepts a Linux friendly name from .github/build/friendly-filenames.json.
EOF
}

fail() { printf 'V2bX: %s\n' "$*" >&2; return 1; }

normalize_version() {
    local version=$1
    valid_version "$version" || return
    printf '%s' "$version"
}

valid_version() {
    [[ $1 =~ ^[a-zA-Z0-9][a-zA-Z0-9._+-]*$ ]] || fail 'Invalid release version.'
}

resolve_latest_version() {
    local effective_url version
    effective_url=$(curl --fail --location --silent --show-error --retry 3 \
        --connect-timeout 15 --max-time 120 --proto '=https' --tlsv1.2 \
        --output /dev/null --write-out '%{url_effective}' \
        "https://github.com/$REPOSITORY/releases/latest") || return
    [[ $effective_url == "https://github.com/$REPOSITORY/releases/tag/"* ]] || { fail 'Could not resolve the latest GitHub release tag.'; return 1; }
    version=${effective_url#"https://github.com/$REPOSITORY/releases/tag/"}
    valid_version "$version" || return
    printf '%s' "$version"
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) printf linux-64 ;;
        i386|i486|i586|i686) printf linux-32 ;;
        aarch64|arm64) printf linux-arm64-v8a ;;
        armv8l|armv7l|armv7*) printf linux-arm32-v7a ;;
        armv6l|armv6*) printf linux-arm32-v6 ;;
        armv5l|armv5*) printf linux-arm32-v5 ;;
        mips64el|mips64le) printf linux-mips64le ;;
        mips64) printf linux-mips64 ;;
        mipsel|mipsle) printf linux-mips32le-softfloat ;;
        mips) printf linux-mips32-softfloat ;;
        ppc64le|riscv64|s390x|ppc64) printf 'linux-%s' "$(uname -m)" ;;
        *) fail 'Unsupported architecture; use --arch with a supported Linux release asset.' ;;
    esac
}

require_runtime() {
    [[ $(uname -s) == Linux ]] || fail 'This installer supports Linux only.' || return
    [[ $(id -u) == 0 ]] || fail 'Run this installer as root.' || return
    local tool
    for tool in curl unzip sha256sum awk mktemp install systemctl; do
        command -v "$tool" >/dev/null 2>&1 || fail "Required command is missing: $tool" || return
    done
    [[ -d /run/systemd/system ]] || fail 'A running systemd installation is required.' || return
}

download() {
    curl --fail --location --silent --show-error --retry 3 \
        --connect-timeout 15 --max-time 300 --proto '=https' --tlsv1.2 \
        --output "$2" "$1"
}

cleanup() {
    local status=$?
    trap - EXIT
    if (( status != 0 )) && $BINARY_REPLACED; then
        if $HAD_BINARY; then
            if mv -f -- "$BACKUP_BINARY" "$INSTALL_DIR/V2bX"; then
                printf 'V2bX: Restored the previous executable.\n' >&2
            else
                printf 'V2bX: Restore failed; previous executable remains at %s\n' "$BACKUP_BINARY" >&2
                BACKUP_BINARY=
            fi
        else
            rm -f -- "$INSTALL_DIR/V2bX"
        fi
        if $CREATED_LINK; then rm -f -- "$BIN_LINK"; fi
        if $CREATED_SERVICE; then
            rm -f -- "$SERVICE_FILE"
            systemctl daemon-reload || true
        fi
        if $WAS_ACTIVE && $HAD_BINARY; then
            systemctl restart V2bX.service || printf 'V2bX: Could not restart the previous service.\n' >&2
        fi
    fi
    [[ -z $STAGED_BINARY ]] || rm -f -- "$STAGED_BINARY"
    [[ -z $BACKUP_BINARY ]] || rm -f -- "$BACKUP_BINARY"
    [[ -z $WORK_DIR ]] || rm -rf -- "$WORK_DIR"
    exit "$status"
}

install_service() {
    # Preserve an existing unit, including local paths, environment and arguments.
    if [[ -e $SERVICE_FILE || -L $SERVICE_FILE ]]; then return; fi
    mkdir -p -- "$(dirname "$SERVICE_FILE")"
    local unit="$WORK_DIR/V2bX.service"
    cat > "$unit" <<EOF
[Unit]
Description=V2bX service
After=network-online.target
Wants=network-online.target
ConditionPathExists=$CONFIG_DIR/config.json

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/V2bX server --config $CONFIG_DIR/config.json
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
    install -m 0644 -- "$unit" "$SERVICE_FILE"
    CREATED_SERVICE=true
}

main() {
    while (( $# )); do
        case "$1" in
            --version|--install-dir|--arch)
                (( $# >= 2 )) || { fail "Missing value for $1"; return 1; }
                case "$1" in
                    --version) VERSION=$(normalize_version "$2") || return ;;
                    --install-dir) INSTALL_DIR=$2 ;;
                    --arch) ARCH=$2 ;;
                esac
                shift 2 ;;
            -h|--help) usage; return ;;
            *) fail "Unknown argument: $1"; return 1 ;;
        esac
    done
    # These paths are also inserted into the systemd unit; reject control and
    # expansion characters rather than producing an ambiguous ExecStart line.
    [[ $INSTALL_DIR =~ ^/[[:alnum:]_./-]+$ && $INSTALL_DIR != / ]] || { fail 'Use an absolute install directory without spaces or shell metacharacters.'; return 1; }
    require_runtime || return
    [[ -n $ARCH ]] || ARCH=$(detect_arch)
    local asset_arch=$ARCH entry=V2bX
    case "$ARCH" in
        linux-64|linux-32|linux-arm64-v8a|linux-arm32-v5|linux-arm32-v6|linux-arm32-v7a|linux-mips64|linux-mips64le|linux-mips32|linux-mips32le|linux-ppc64|linux-ppc64le|linux-riscv64|linux-s390x) ;;
        linux-mips32-softfloat|linux-mips32le-softfloat)
            asset_arch=${ARCH%-softfloat}
            entry=V2bX_softfloat ;;
        *) fail "Unsupported Linux release architecture: $ARCH"; return 1 ;;
    esac
    local asset="V2bX-$asset_arch.zip" base_url
    [[ -n $VERSION ]] || VERSION=$(resolve_latest_version)
    base_url="https://github.com/$REPOSITORY/releases/download/$VERSION"
    # mktemp creates a private staging directory; new program/service directories
    # should remain traversable, while installed configuration files use 0600.
    umask 022
    WORK_DIR=$(mktemp -d)
    trap cleanup EXIT
    download "$base_url/$asset" "$WORK_DIR/release.zip"
    download "$base_url/$asset.dgst" "$WORK_DIR/release.dgst"
    local expected actual
    expected=$(awk 'toupper($0) ~ /^SHA(2-)?256[[:space:]]*=/ { print $NF }' "$WORK_DIR/release.dgst")
    [[ $expected =~ ^[[:xdigit:]]{64}$ ]] || { fail 'Release checksum has no unique SHA256 entry.'; return 1; }
    actual=$(sha256sum "$WORK_DIR/release.zip")
    actual=${actual%% *}
    [[ ${actual,,} == ${expected,,} ]] || { fail 'Release SHA256 verification failed; the installed executable was not changed.'; return 1; }
    [[ $(unzip -Z1 "$WORK_DIR/release.zip" | awk -v name="$entry" '$0 == name {count++} END {print count+0}') == 1 ]] || { fail "Release archive must contain exactly one $entry executable."; return 1; }
    unzip -p "$WORK_DIR/release.zip" "$entry" > "$WORK_DIR/V2bX"
    chmod 0755 "$WORK_DIR/V2bX"
    "$WORK_DIR/V2bX" version

    local first_install=true
    if [[ -e $INSTALL_DIR/V2bX || -L $INSTALL_DIR/V2bX ]]; then
        HAD_BINARY=true
        first_install=false
    fi
    if systemctl is-active --quiet V2bX.service; then WAS_ACTIVE=true; fi
    mkdir -p -- "$INSTALL_DIR" "$CONFIG_DIR"
    STAGED_BINARY=$(mktemp "$INSTALL_DIR/.V2bX.new.XXXXXX")
    install -m 0755 -- "$WORK_DIR/V2bX" "$STAGED_BINARY"
    if $HAD_BINARY; then
        BACKUP_BINARY=$(mktemp "$INSTALL_DIR/.V2bX.previous.XXXXXX")
        cp -p -- "$INSTALL_DIR/V2bX" "$BACKUP_BINARY"
    fi
    mv -f -- "$STAGED_BINARY" "$INSTALL_DIR/V2bX"
    STAGED_BINARY=
    BINARY_REPLACED=true
    install_service
    if [[ $BIN_LINK != "$INSTALL_DIR/V2bX" && ! -e $BIN_LINK && ! -L $BIN_LINK ]]; then
        mkdir -p -- "$(dirname "$BIN_LINK")"
        ln -s -- "$INSTALL_DIR/V2bX" "$BIN_LINK"
        CREATED_LINK=true
    fi
    if $first_install; then
        local filename
        for filename in config.json sing_origin.json; do
            if [[ ! -e $CONFIG_DIR/$filename && ! -L $CONFIG_DIR/$filename ]] && \
                unzip -Z1 "$WORK_DIR/release.zip" | awk -v name="$filename" '$0 == name {found=1} END {exit !found}'; then
                unzip -p "$WORK_DIR/release.zip" "$filename" > "$WORK_DIR/$filename"
                install -m 0600 -- "$WORK_DIR/$filename" "$CONFIG_DIR/$filename"
            fi
        done
    fi
    systemctl daemon-reload
    if $WAS_ACTIVE; then systemctl restart V2bX.service; fi
    BINARY_REPLACED=false
    printf 'V2bX: Installed %s from github.com/%s.\n' "${VERSION:-latest release}" "$REPOSITORY"
    if ! $WAS_ACTIVE; then
        printf 'V2bX: Service remains stopped. Review %s/config.json, then enable/start V2bX.service when ready.\n' "$CONFIG_DIR"
    fi
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then main "$@"; fi
