#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
PROFILES_DIR="${ROOT_DIR}/scripts/images/alpine/profiles"

PROFILE="balanced"
ALPINE_BRANCH="v3.23"
IMAGE_NAME="alpine-agentic.ext4"
WORKDIR=""
KEEP_WORKDIR="false"
OVERHEAD_PERCENT="35"
MIN_OVERHEAD_MB="256"
SHRINK_IMAGE="true"

if [[ ${EUID} -eq 0 && -n "${SUDO_USER:-}" ]]; then
	INVOKING_HOME="$(getent passwd "${SUDO_USER}" 2>/dev/null | cut -d: -f6 || true)"
	if [[ -z "${INVOKING_HOME}" ]]; then
		INVOKING_HOME="${HOME}"
	fi
else
  INVOKING_HOME="${HOME}"
fi

OUTPUT_DIR="${INVOKING_HOME}/.sbx/images"

REQUIRED_PACKAGES=(openssh openrc e2fsprogs-extra)

usage() {
  cat <<'EOF'
Usage: build-rootfs.sh [options]

Build an Alpine ext4 rootfs image for SBX Firecracker sandboxes.

Options:
  --profile NAME          Package profile: minimal|balanced|heavy (default: balanced)
  --branch NAME           Alpine branch for alpine-make-rootfs (default: v3.23)
  --output-dir PATH       Output directory for ext4 image (default: ~/.sbx/images)
  --image-name NAME       Output ext4 file name (default: alpine-agentic.ext4)
  --workdir PATH          Temporary build directory (default: auto tmp dir)
  --keep-workdir          Keep build directory after completion
  --overhead-percent N    Extra image size percent over rootfs (default: 35)
  --min-overhead-mb N     Minimum extra MB added to image (default: 256)
  --no-shrink             Skip post-build filesystem/image shrinking
  --help, -h              Show this help message

Environment:
  ALPINE_MAKE_ROOTFS_BIN  Path to alpine-make-rootfs executable

Examples:
  ./scripts/images/alpine/build-rootfs.sh
  ./scripts/images/alpine/build-rootfs.sh --profile heavy --image-name alpine-heavy.ext4
EOF
}

log() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*"
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILE="$2"
      shift 2
      ;;
    --branch)
      ALPINE_BRANCH="$2"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --image-name)
      IMAGE_NAME="$2"
      shift 2
      ;;
    --workdir)
      WORKDIR="$2"
      shift 2
      ;;
    --keep-workdir)
      KEEP_WORKDIR="true"
      shift
      ;;
    --overhead-percent)
      OVERHEAD_PERCENT="$2"
      shift 2
      ;;
    --min-overhead-mb)
      MIN_OVERHEAD_MB="$2"
      shift 2
      ;;
    --no-shrink)
      SHRINK_IMAGE="false"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

PROFILE_FILE="${PROFILES_DIR}/${PROFILE}.txt"
if [[ ! -f "${PROFILE_FILE}" ]]; then
  die "Unknown profile '${PROFILE}'. Expected one of: minimal, balanced, heavy"
fi

if [[ -z "${WORKDIR}" ]]; then
  WORKDIR="$(mktemp -d -t sbx-alpine-rootfs-XXXXXX)"
else
  mkdir -p "${WORKDIR}"
fi

MOUNT_DIR="${WORKDIR}/mnt"
ROOTFS_DIR="${WORKDIR}/rootfs"
EXT4_PATH="${WORKDIR}/${IMAGE_NAME}"
OUTPUT_PATH="${OUTPUT_DIR}/${IMAGE_NAME}"

if [[ "${KEEP_WORKDIR}" != "true" ]]; then
  cleanup() {
    if mountpoint -q "${MOUNT_DIR}"; then
      ${SUDO:-} umount "${MOUNT_DIR}" >/dev/null 2>&1 || true
    fi
    if ! rm -rf "${WORKDIR}" 2>/dev/null; then
      warn "Could not remove temporary workdir due permissions, left at: ${WORKDIR}"
      warn "Remove manually if desired: sudo rm -rf '${WORKDIR}'"
    fi
  }
  trap cleanup EXIT
fi

if [[ ${EUID} -eq 0 ]]; then
  SUDO=""
else
  command -v sudo >/dev/null 2>&1 || die "This script requires root privileges (sudo not found)"
  SUDO="sudo"
fi

resolve_alpine_make_rootfs() {
  if [[ -n "${ALPINE_MAKE_ROOTFS_BIN:-}" ]]; then
    [[ -x "${ALPINE_MAKE_ROOTFS_BIN}" ]] || die "ALPINE_MAKE_ROOTFS_BIN is not executable: ${ALPINE_MAKE_ROOTFS_BIN}"
    printf '%s' "${ALPINE_MAKE_ROOTFS_BIN}"
    return
  fi

  if command -v alpine-make-rootfs >/dev/null 2>&1; then
    command -v alpine-make-rootfs
    return
  fi

  local tool_dir="${WORKDIR}/alpine-make-rootfs"
  printf '[INFO] %s\n' "alpine-make-rootfs not found, cloning helper repository" >&2
  git clone --depth 1 https://github.com/alpinelinux/alpine-make-rootfs.git "${tool_dir}" >/dev/null 2>&1
  chmod +x "${tool_dir}/alpine-make-rootfs"
  printf '%s' "${tool_dir}/alpine-make-rootfs"
}

read_profile_packages() {
  local file="$1"
  local packages=()
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line//[$'\t\r\n ']/}"
    [[ -z "${line}" ]] && continue
    packages+=("${line}")
  done <"${file}"

  printf '%s\n' "${packages[@]}"
}

append_if_missing() {
  local pattern="$1"
  local line="$2"
  local file="$3"
  if ! ${SUDO} grep -Eq "${pattern}" "${file}"; then
    printf '%s\n' "${line}" | ${SUDO} tee -a "${file}" >/dev/null
  fi
}

maybe_shrink_image() {
  local image_path="$1"
  if [[ "${SHRINK_IMAGE}" != "true" ]]; then
    return
  fi

  if ! command -v e2fsck >/dev/null 2>&1 || ! command -v resize2fs >/dev/null 2>&1 || ! command -v dumpe2fs >/dev/null 2>&1; then
    warn "Skipping image shrink (missing required host tools: e2fsck/resize2fs/dumpe2fs)"
    return
  fi

  log "Shrinking filesystem to minimum size"
  ${SUDO} e2fsck -fy "${image_path}" >/dev/null 2>&1
  ${SUDO} resize2fs -M "${image_path}" >/dev/null

  local block_count
  local block_size
  block_count="$(${SUDO} dumpe2fs -h "${image_path}" 2>/dev/null | awk -F: '/Block count:/ {gsub(/^[[:space:]]+/, "", $2); print $2; exit}')"
  block_size="$(${SUDO} dumpe2fs -h "${image_path}" 2>/dev/null | awk -F: '/Block size:/ {gsub(/^[[:space:]]+/, "", $2); print $2; exit}')"

  if [[ -z "${block_count}" || -z "${block_size}" ]]; then
    warn "Could not determine ext4 geometry for truncate, leaving sparse image as-is"
    return
  fi

  local fs_bytes
  local pad_bytes
  local final_bytes
  fs_bytes=$((block_count * block_size))
  pad_bytes=$((8 * 1024 * 1024))
  final_bytes=$((fs_bytes + pad_bytes))
  truncate -s "${final_bytes}" "${image_path}"

  log "Shrunk image size to $((final_bytes / 1024 / 1024)) MB"
}

ALPINE_MAKE_ROOTFS="$(resolve_alpine_make_rootfs)"

mapfile -t PROFILE_PACKAGES < <(read_profile_packages "${PROFILE_FILE}")

declare -A seen=()
ALL_PACKAGES=()
for p in "${REQUIRED_PACKAGES[@]}" "${PROFILE_PACKAGES[@]}"; do
  if [[ -z "${seen[$p]:-}" ]]; then
    seen[$p]=1
    ALL_PACKAGES+=("$p")
  fi
done
PACKAGES_STR="${ALL_PACKAGES[*]}"

log "Profile: ${PROFILE}"
log "Alpine branch: ${ALPINE_BRANCH}"
log "Output: ${OUTPUT_PATH}"
log "Using alpine-make-rootfs: ${ALPINE_MAKE_ROOTFS}"

mkdir -p "${ROOTFS_DIR}" "${MOUNT_DIR}" "${OUTPUT_DIR}"

log "Building rootfs with alpine-make-rootfs"
${SUDO} "${ALPINE_MAKE_ROOTFS}" --branch "${ALPINE_BRANCH}" --packages "${PACKAGES_STR}" "${ROOTFS_DIR}"

SIZE_MB="$(${SUDO} du -sm "${ROOTFS_DIR}" | cut -f1)"
EXTRA_MB=$((SIZE_MB * OVERHEAD_PERCENT / 100))
if (( EXTRA_MB < MIN_OVERHEAD_MB )); then
  EXTRA_MB=${MIN_OVERHEAD_MB}
fi
TOTAL_MB=$((SIZE_MB + EXTRA_MB))

log "Rootfs size: ${SIZE_MB} MB"
log "Image overhead: ${EXTRA_MB} MB (${OVERHEAD_PERCENT}%, min ${MIN_OVERHEAD_MB} MB)"
log "Creating ext4 image (${TOTAL_MB} MB)"
dd if=/dev/zero of="${EXT4_PATH}" bs=1M count="${TOTAL_MB}" status=none
mkfs.ext4 -q "${EXT4_PATH}"

log "Copying rootfs into ext4 image"
${SUDO} mount "${EXT4_PATH}" "${MOUNT_DIR}"
${SUDO} cp -a "${ROOTFS_DIR}"/. "${MOUNT_DIR}/"

log "Configuring OpenSSH and SBX hook directories"
${SUDO} chroot "${MOUNT_DIR}" rc-update add sshd default >/dev/null
${SUDO} chroot "${MOUNT_DIR}" passwd -d root >/dev/null

if ! ${SUDO} chroot "${MOUNT_DIR}" /bin/sh -c 'command -v apk >/dev/null 2>&1'; then
  die "apk not found in built rootfs. Ensure apk-tools is available in selected profile."
fi

SSHD_CONFIG="${MOUNT_DIR}/etc/ssh/sshd_config"
append_if_missing '^PermitRootLogin[[:space:]]+yes$' 'PermitRootLogin yes' "${SSHD_CONFIG}"
append_if_missing '^PermitEmptyPasswords[[:space:]]+yes$' 'PermitEmptyPasswords yes' "${SSHD_CONFIG}"
append_if_missing '^PermitUserRC[[:space:]]+yes$' 'PermitUserRC yes' "${SSHD_CONFIG}"

${SUDO} mkdir -p "${MOUNT_DIR}/etc/sbx/hooks/start.d"
${SUDO} install -d -m 0755 "${MOUNT_DIR}/usr/local/bin"

${SUDO} tee "${MOUNT_DIR}/usr/local/bin/sbx-start-hooks" >/dev/null <<'EOF'
#!/bin/sh
set -eu

HOOK_DIR="/etc/sbx/hooks/start.d"

[ -d "${HOOK_DIR}" ] || exit 0

for hook in "${HOOK_DIR}"/*; do
    [ -e "${hook}" ] || continue
    [ -x "${hook}" ] || continue
    "${hook}"
done
EOF
${SUDO} chmod 0755 "${MOUNT_DIR}/usr/local/bin/sbx-start-hooks"

${SUDO} umount "${MOUNT_DIR}"

maybe_shrink_image "${EXT4_PATH}"

mv "${EXT4_PATH}" "${OUTPUT_PATH}"

if [[ ${EUID} -eq 0 && -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
  chown "${SUDO_UID}:${SUDO_GID}" "${OUTPUT_PATH}"
fi

log "Built image: ${OUTPUT_PATH}"
log "Done"
