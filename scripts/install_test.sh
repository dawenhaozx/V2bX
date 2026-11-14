#!/usr/bin/env bash
# Offline tests: all paths are temporary and root, downloads, unzip and systemd
# are mocked. This script never installs or operates a real service.
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT

assert_equal() {
    [[ $1 == "$2" ]] || { printf 'FAIL: %s (got %q, wanted %q)\n' "$3" "$1" "$2" >&2; exit 1; }
}

run_case() (
    name=$1
    # shellcheck source=install.sh
    source "$SCRIPT_DIR/install.sh"
    test_dir="$TEST_ROOT/$name"
    mkdir -p "$test_dir"
    INSTALL_DIR="$test_dir/program"
    CONFIG_DIR="$test_dir/config"
    SERVICE_FILE="$test_dir/systemd/V2bX.service"
    BIN_LINK="$test_dir/bin/V2bX"
    WORK_DIR=
    payload="$test_dir/payload"
    printf 'mock release archive\n' > "$payload"
    checksum=$(sha256sum "$payload")
    checksum=${checksum%% *}
    printf 'SHA2-256= %s\n' "$checksum" > "$test_dir/checksum"
    active=false
    if [[ $name != fresh ]]; then
        mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$(dirname "$SERVICE_FILE")"
        printf 'old executable\n' > "$INSTALL_DIR/V2bX"
        printf 'existing config\n' > "$CONFIG_DIR/config.json"
        printf 'existing origin\n' > "$CONFIG_DIR/sing_origin.json"
        printf 'private certificate\n' > "$CONFIG_DIR/cert.pem"
        printf 'existing custom unit\n' > "$SERVICE_FILE"
        active=true
    fi
    if [[ $name == checksum_failure ]]; then
        printf 'SHA256= %064d\n' 0 > "$test_dir/checksum"
    fi
    require_runtime() { :; }
    curl() {
        [[ ${!#} == 'https://github.com/dawenhaozx/V2bX/releases/latest' ]] || return 1
        printf 'resolved\n' >> "$test_dir/latest_requests"
        printf 'https://github.com/dawenhaozx/V2bX/releases/tag/v4.5.6'
    }
    download() {
        printf '%s\n' "$1" >> "$test_dir/urls"
        if [[ $name == download_failure ]]; then return 1; fi
        case "$1" in
            *.zip.dgst) cp "$test_dir/checksum" "$2" ;;
            *.zip) cp "$payload" "$2" ;;
            *) printf 'Unexpected download URL\n' >&2; return 1 ;;
        esac
    }
    unzip() {
        case "$1" in
            -Z1) printf 'V2bX\nV2bX_softfloat\nconfig.json\nsing_origin.json\n' ;;
            -p)
                case "$3" in
                    V2bX|V2bX_softfloat)
                        if [[ $name == binary_failure ]]; then
                            printf '#!/usr/bin/env bash\nexit 1\n'
                        else
                            printf '#!/usr/bin/env bash\nprintf "new executable\\n"\n'
                        fi ;;
                    config.json) printf '{"Cores":[],"Nodes":[]}\n' ;;
                    sing_origin.json) printf '{"inbounds":[],"outbounds":[]}\n' ;;
                    *) return 1 ;;
                esac ;;
            *) return 1 ;;
        esac
    }
    systemctl() {
        printf '%s\n' "$*" >> "$test_dir/systemctl"
        case "$1" in
            is-active) $active ;;
            daemon-reload) return 0 ;;
            restart)
                if [[ $name == restart_failure && ! -e $test_dir/restart_failed ]]; then
                    touch "$test_dir/restart_failed"
                    return 1
                fi ;;
            *) printf 'Forbidden service operation: %s\n' "$*" >&2; return 1 ;;
        esac
    }
    if [[ $name == latest ]]; then
        main --arch linux-64
    else
        main --arch linux-64 --version v1.2.3
    fi
)

for case_name in fresh update latest checksum_failure download_failure binary_failure restart_failure; do
    set +e
    run_case "$case_name" > "$TEST_ROOT/$case_name.log" 2>&1
    status=$?
    set -e
    case "$case_name" in
        fresh|update|latest)
            if [[ $status != 0 ]]; then cat "$TEST_ROOT/$case_name.log"; fi
            assert_equal "$status" 0 "$case_name exit status" ;;
        *) [[ $status != 0 ]] || { printf 'FAIL: %s unexpectedly succeeded\n' "$case_name" >&2; exit 1; } ;;
    esac
    case_dir="$TEST_ROOT/$case_name"
    if [[ $case_name == fresh || $case_name == update || $case_name == latest ]]; then
        [[ $(cat "$case_dir/program/V2bX") == *'new executable'* ]] || { cat "$TEST_ROOT/$case_name.log"; exit 1; }
    else
        assert_equal "$(cat "$case_dir/program/V2bX")" 'old executable' "$case_name preserves executable"
    fi
    if [[ $case_name != fresh ]]; then
        assert_equal "$(cat "$case_dir/config/config.json")" 'existing config' "$case_name preserves config"
        assert_equal "$(cat "$case_dir/config/sing_origin.json")" 'existing origin' "$case_name preserves origin"
        assert_equal "$(cat "$case_dir/config/cert.pem")" 'private certificate' "$case_name preserves certificate"
        assert_equal "$(cat "$case_dir/systemd/V2bX.service")" 'existing custom unit' "$case_name preserves service"
    else
        [[ -s $case_dir/config/config.json && -s $case_dir/config/sing_origin.json && -e $case_dir/bin/V2bX ]] || exit 1
        if awk '/^(start|restart|enable)( |$)/ {found=1} END {exit !found}' "$case_dir/systemctl"; then
            printf 'FAIL: new installation started a service\n' >&2; exit 1
        fi
    fi
    expected_version=v1.2.3
    if [[ $case_name == latest ]]; then
        expected_version=v4.5.6
        assert_equal "$(wc -l < "$case_dir/latest_requests" | tr -d ' ')" 1 'latest resolved once'
    fi
    assert_equal "$(head -n 1 "$case_dir/urls")" "https://github.com/dawenhaozx/V2bX/releases/download/$expected_version/V2bX-linux-64.zip" "$case_name archive version"
    if [[ $case_name != download_failure ]]; then
        assert_equal "$(tail -n 1 "$case_dir/urls")" "https://github.com/dawenhaozx/V2bX/releases/download/$expected_version/V2bX-linux-64.zip.dgst" "$case_name checksum version"
    fi
    printf 'PASS: %s\n' "$case_name"
done

(
    source "$SCRIPT_DIR/install.sh"
    uname() { printf armv7l; }
    assert_equal "$(detect_arch)" linux-arm32-v7a 'ARMv7 asset name'
    uname() { printf mipsel; }
    assert_equal "$(detect_arch)" linux-mips32le-softfloat 'MIPS softfloat asset selection'
)
printf 'All offline installer tests passed.\n'
