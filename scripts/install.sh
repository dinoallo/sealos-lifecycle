#!/bin/sh
# Copyright © 2023 sealos.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e
set -o noglob

# Usage:
#   curl ... | ENV_VAR=... sh -
#   curl -sfL https://raw.githubusercontent.com/labring/sealos/main/scripts/install.sh | sh -s latest labring/sealos
#
FILE_NAME=sealos
BIN_DIR=/usr/bin
INSTALL=

info() {
    echo '[INFO] ' "$@"
}

warn() {
    echo '[WARN] ' "$@" >&2
}

fatal() {
    echo '[ERROR] ' "$@" >&2
    exit 1
}

verify_url() {
    case "${SEALOS_URL}" in
    "") ;;
    https://*) ;;
    *)
        fatal "Only https:// URLs are supported for SEALOS_URL (have ${SEALOS_URL})"
        ;;
    esac
}

verify_is_executable() {
    if [ ! -x "${BIN_DIR}/sealos" ]; then
        fatal "Executable sealos binary not found at ${BIN_DIR}/sealos"
    fi
}

setup_verify_arch() {
    if [ -z "$ARCH" ]; then
        ARCH=$(uname -m)
    fi
    case $ARCH in
    amd64 | x86_64)
        ARCH=amd64
        SUFFIX=
        ;;
    arm64 | aarch64)
        ARCH=arm64
        SUFFIX=-${ARCH}
        ;;
    *)
        fatal "Unsupported architecture $ARCH"
        ;;
    esac
}

verify_downloader() {
    [ -x "$(command -v "$1")" ] || return 1
    DOWNLOADER=$1
    DOWNLOADER_PREFIX=https://github.com/${OWN_REPO}/releases/download/
    if [ -n "$PROXY_PREFIX" ]; then
        DOWNLOADER_PREFIX="${PROXY_PREFIX%/}/${DOWNLOADER_PREFIX}"
    fi
    ARCHIVE_NAME=${FILE_NAME}_${VERSION##v}_linux_${ARCH}.tar.gz
    DOWNLOADER_URL=${DOWNLOADER_PREFIX}${VERSION}/${ARCHIVE_NAME}
    CHECKSUM_URL=${DOWNLOADER_PREFIX}${VERSION}/${FILE_NAME}_checksums.txt
    return 0
}

get_release_version() {
    VERSION=$1
    if [ -z "$VERSION" ]; then
        VERSION=latest
    fi
    OWN_REPO=$2
    if [ -z "$OWN_REPO" ]; then
        warn "OWN_REPO is empty, using default repo: labring/sealos"
        OWN_REPO=labring/sealos
    fi
    if [ "$VERSION" = "latest" ]; then
        VERSION=$(curl -s https://api.github.com/repos/${OWN_REPO}/releases/latest | grep tag_name | cut -d '"' -f 4)
    fi
    info "Using ${VERSION} as release"
    info "Using ${OWN_REPO} as your repo"
}

download() {
    [ $# -eq 2 ] || fatal 'download needs exactly 2 arguments'
    info "Downloading sealos, waiting..."
    case $DOWNLOADER in
    curl)
        status_code=$(curl -L "$2" -o "$1" --progress-bar -w "%{http_code}\n") || fatal 'Download failed'
        if [ "$status_code" != "200" ]; then
            fatal "Download failed, status code: $status_code"
        fi
        ;;
    wget)
        wget -qO "$1" "$2" || fatal 'Download failed'
        ;;
    *)
        fatal "Incorrect executable '$DOWNLOADER'"
        ;;
    esac
}

download_binary() {
    info "Downloading tar ${DOWNLOADER} ${DOWNLOADER_URL}"
    download "${TMP_TAR}" "${DOWNLOADER_URL}"
}

verify_checksum() {
    if [ "${SEALOS_INSTALL_SKIP_CHECKSUM:-false}" = "true" ]; then
        warn "Skipping release checksum verification because SEALOS_INSTALL_SKIP_CHECKSUM=true"
        return 0
    fi

    info "Downloading checksum ${DOWNLOADER} ${CHECKSUM_URL}"
    download "${TMP_CHECKSUM}" "${CHECKSUM_URL}"

    expected_checksum=$(awk -v name="${ARCHIVE_NAME}" '$2 == name {print $1}' "${TMP_CHECKSUM}")
    if [ -z "${expected_checksum}" ]; then
        fatal "Checksum for ${ARCHIVE_NAME} not found in ${FILE_NAME}_checksums.txt"
    fi

    if [ -x "$(command -v sha256sum)" ]; then
        actual_checksum=$(sha256sum "${TMP_TAR}" | awk '{print $1}')
    elif [ -x "$(command -v shasum)" ]; then
        actual_checksum=$(shasum -a 256 "${TMP_TAR}" | awk '{print $1}')
    else
        fatal "Can not find sha256sum or shasum for checksum verification"
    fi

    if [ "${actual_checksum}" != "${expected_checksum}" ]; then
        fatal "Checksum verification failed for ${ARCHIVE_NAME}"
    fi

    info "Verified checksum for ${ARCHIVE_NAME}"
}

setup_tmp() {
    TMP_DIR=$(mktemp -d -t sealos-install.XXXXXXXXXX)
    TMP_TAR=${TMP_DIR}/sealos.tar.gz
    TMP_CHECKSUM=${TMP_DIR}/sealos_checksums.txt
    TMP_BIN=${TMP_DIR}/sealos
    cleanup() {
        code=$?
        set +e
        trap - EXIT
        rm -rf "${TMP_DIR}"
        if [ $code -ne 0 ]; then
            warn "Failed to install sealos"
        fi
        exit $code
    }
    trap cleanup INT EXIT
}

cleanup_tmp() {
    rm -rf "${TMP_DIR}"
}

setup_binary() {
    cd "${TMP_DIR}"
    tar -zxf "${TMP_TAR}" "${FILE_NAME}"
    chmod 755 "${FILE_NAME}"
    info "Installing sealos to ${BIN_DIR}/${FILE_NAME}"
    if [ "$(id -u)" -eq 0 ]; then
        INSTALL="install"
    else
        if ! [ -x "$(command -v sudo)" ]; then
            fatal "sudo is required to install sealos to ${BIN_DIR}"
        fi
        INSTALL="sudo_install"
    fi
    if [ "${INSTALL}" = "sudo_install" ]; then
        sudo install -d -m 0755 "${BIN_DIR}"
        sudo install -o root -g root -m 0755 "${TMP_BIN}" "${BIN_DIR}/${FILE_NAME}"
    else
        install -d -m 0755 "${BIN_DIR}"
        install -o root -g root -m 0755 "${TMP_BIN}" "${BIN_DIR}/${FILE_NAME}"
    fi
}

verify_binary() {
    "${BIN_DIR}/${FILE_NAME}" version || fatal 'failed to verify binary'
}

{
    verify_url
    setup_verify_arch
    get_release_version "$1" "$2"
    verify_downloader curl || verify_downloader wget || fatal 'Can not find curl or wget for downloading files'
    setup_tmp
    download_binary
    verify_checksum
    setup_binary
    verify_binary
    cleanup_tmp
}
