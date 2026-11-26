#!/usr/bin/env bash
set -euo pipefail

repository="${CELLBOX_REPOSITORY:-Senasphy/cellbox}"
install_dir="${CELLBOX_INSTALL_DIR:-${HOME}/.local/bin}"
requested_version="${1:-latest}"

for command in curl tar sha256sum install uname mktemp awk tr mkdir chmod mv; do
  command -v "$command" >/dev/null 2>&1 || {
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  }
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) ;;
  *) printf 'Cellbox release binaries are currently available for Linux only (got %s).\n' "$os" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) printf 'Unsupported CPU architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
curl_args=(--fail --location --silent --show-error --retry 2)
token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [[ -n "$token" ]]; then
  if [[ ! "$token" =~ ^[A-Za-z0-9_-]+$ ]]; then
    printf 'GitHub token contains characters that cannot be used safely in curl configuration.\n' >&2
    exit 1
  fi
  printf 'header = "Authorization: Bearer %s"\n' "$token" > "${temporary}/curl.conf"
  chmod 0600 "${temporary}/curl.conf"
  curl_args+=(--config "${temporary}/curl.conf")
fi

if [[ "$requested_version" == latest ]]; then
  latest_url=$(curl "${curl_args[@]}" --output /dev/null --write-out '%{url_effective}' "https://github.com/${repository}/releases/latest") || {
    printf 'No published GitHub release found for %s yet. Build from source with `make build`, or pass an explicit release version.\n' "$repository" >&2
    exit 1
  }
  tag=${latest_url##*/}
  asset_version="${tag#v}"
  if [[ "$tag" == "$latest_url" || ! "$asset_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf 'No published GitHub release tag found for %s (resolved %s). Build from source with `make build`, or pass an explicit release version.\n' "$repository" "$latest_url" >&2
    exit 1
  fi
  release_url="https://github.com/${repository}/releases/download/${tag}"
else
  requested_version="${requested_version#v}"
  if [[ ! "$requested_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    printf 'Version must be latest or a semantic version (got %s).\n' "$requested_version" >&2
    exit 1
  fi
  release_url="https://github.com/${repository}/releases/download/v${requested_version}"
  asset_version="$requested_version"
fi

asset="cellbox_${asset_version}_linux_${arch}.tar.gz"
checksums="cellbox_checksums.txt"
curl "${curl_args[@]}" "${release_url}/${checksums}" -o "${temporary}/${checksums}"
curl "${curl_args[@]}" "${release_url}/${asset}" -o "${temporary}/${asset}"

if ! awk -v wanted="$asset" '$2 == wanted { print; found=1 } END { exit !found }' "${temporary}/${checksums}" > "${temporary}/asset.sha256"; then
  printf 'Checksum manifest does not contain %s.\n' "$asset" >&2
  exit 1
fi
(cd "$temporary" && sha256sum --check --status asset.sha256) || {
  printf 'Checksum verification failed for %s.\n' "$asset" >&2
  exit 1
}

tar -tzf "${temporary}/${asset}" | awk 'BEGIN { ok=0 } $0 == "cellbox" { ok=1 } END { exit !ok }' || {
  printf 'Release archive does not contain the expected cellbox binary.\n' >&2
  exit 1
}
mkdir -p "$install_dir"
tar -xOzf "${temporary}/${asset}" cellbox > "${temporary}/cellbox"
chmod 0755 "${temporary}/cellbox"
install -m 0755 "${temporary}/cellbox" "${install_dir}/.cellbox-new-$$"
mv -f "${install_dir}/.cellbox-new-$$" "${install_dir}/cellbox"

printf 'Installed Cellbox in %s/cellbox\n' "$install_dir"
if [[ ":${PATH}:" != *":${install_dir}:"* ]]; then
  printf 'Add %s to PATH to run cellbox.\n' "$install_dir"
fi
