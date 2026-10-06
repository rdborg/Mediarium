#!/usr/bin/env bash
# Prints the GitHub release notes for one version: how to install or update,
# then that version's section of CHANGELOG.md. Used by the release workflow.
#
#   tools/release-notes.sh 2.1.0 > notes.md
set -euo pipefail

version="${1:?usage: tools/release-notes.sh <version>}"
cd "$(dirname "$0")/.."

section="$(awk -v v="${version}" '
  $0 ~ "^## \\[" v "\\]" { found = 1; next }
  found && /^## \[/ { exit }
  found { print }
' CHANGELOG.md | sed -e '/./,$!d')"

if [ -z "${section}" ]; then
  echo "No section for ${version} in CHANGELOG.md." >&2
  exit 1
fi

cat <<EOF
**Install or update:** the Docker image is \`ghcr.io/rdborg/mediarium:${version}\` (also \`:latest\`), for \`linux/amd64\` and \`linux/arm64\`. A running Mediarium offers this version under Settings > System, where **Update now** installs it. New here? Start with the [install guide](https://github.com/rdborg/Mediarium/blob/main/docs/INSTALL.md) or [mediarium.app](https://mediarium.app).

${section}
EOF
