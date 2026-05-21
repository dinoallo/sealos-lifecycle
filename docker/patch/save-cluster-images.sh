#!/bin/bash

set -euo pipefail

: "${OWNER:?OWNER is required}"
: "${GIT_COMMIT_SHORT_SHA:?GIT_COMMIT_SHORT_SHA is required}"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PATCH_DIR=${SCRIPT_DIR}
SEALOS_VERSION=4.1.7

if [ -z "${ARCH:-}" ]; then
  ARCH=$(uname -m)
fi

case "${ARCH}" in
  amd64|x86_64)
    ARCH=amd64
    SEALOS_ARCH=amd64
    ;;
  arm64|aarch64)
    ARCH=arm64
    SEALOS_ARCH=arm64
    ;;
  *)
    echo "Unsupported ARCH: ${ARCH}" >&2
    exit 1
    ;;
esac

IMAGE=localhost:5000/${OWNER}/lvscare:$GIT_COMMIT_SHORT_SHA-$ARCH
PATCH=ghcr.io/${OWNER}/sealos-patch:$GIT_COMMIT_SHORT_SHA-$ARCH
SEALOS_RELEASE_URL=https://github.com/labring/sealos/releases/download/v${SEALOS_VERSION}
SEALOS_ARCHIVE=sealos_${SEALOS_VERSION}_linux_${SEALOS_ARCH}.tar.gz
SEALOS_CHECKSUM_FILE=sealos_checksums.txt

# download sealos
wget -O "${SEALOS_ARCHIVE}" "${SEALOS_RELEASE_URL}/${SEALOS_ARCHIVE}"
wget -O "${SEALOS_CHECKSUM_FILE}" "${SEALOS_RELEASE_URL}/${SEALOS_CHECKSUM_FILE}"
expected_checksum=$(awk -v name="${SEALOS_ARCHIVE}" '$2 == name {print $1}' "${SEALOS_CHECKSUM_FILE}")
if [ -z "${expected_checksum}" ]; then
  echo "Checksum for ${SEALOS_ARCHIVE} not found in ${SEALOS_CHECKSUM_FILE}" >&2
  exit 1
fi
actual_checksum=$(sha256sum "${SEALOS_ARCHIVE}" | awk '{print $1}')
if [ "${actual_checksum}" != "${expected_checksum}" ]; then
  echo "Checksum verification failed for ${SEALOS_ARCHIVE}" >&2
  exit 1
fi
tar -zxf "${SEALOS_ARCHIVE}" sealos
chmod +x sealos && sudo mv sealos /usr/bin/sealos

# resolve buildah conflicts
sudo apt remove -y buildah
sudo apt autoremove -y

# use correct image name
# shellcheck disable=SC2164
cd "${PATCH_DIR}"
mkdir -p images/shim
echo "${IMAGE}" > images/shim/lvscareImage
sed -i "s#__lvscare__#${IMAGE}#g" Dockerfile

sudo sealos build -t "${PATCH}" --label=sealos.io.type=patch --label=image="${IMAGE}" --platform linux/"${ARCH}" -f Dockerfile .

# save patch image
cd - && sudo sealos save -o patch-"${ARCH}".tar "${PATCH}"
