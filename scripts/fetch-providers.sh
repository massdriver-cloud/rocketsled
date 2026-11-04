#!/usr/bin/env bash
# Pre-populates the OpenTofu provider cache Rocketsled installs use, so a catalog
# bootstrap never downloads a provider at install time. Usage:
#   scripts/fetch-providers.sh [cache-dir] [platform]
# Defaults: data/plugin-cache and the current machine.
set -euo pipefail

CACHE=${1:-data/plugin-cache}
PLATFORM=${2:-$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')}

# namespace/type version
PROVIDERS="massdriver-cloud/massdriver 2.4.0"

echo "$PROVIDERS" | while read -r source version; do
  type=${source#*/}
  dir="$CACHE/registry.opentofu.org/$source/$version/$PLATFORM"
  if [ -x "$dir/terraform-provider-${type}_v$version" ]; then
    echo "cached  $source $version $PLATFORM"
    continue
  fi
  mkdir -p "$dir"
  url="https://github.com/${source%%/*}/terraform-provider-$type/releases/download/v$version/terraform-provider-${type}_${version}_${PLATFORM}.zip"
  echo "fetch   $url"
  tmp=$(mktemp)
  curl -fsSL --retry 3 -o "$tmp" "$url"
  unzip -o -q "$tmp" -d "$dir"
  rm -f "$tmp"
  chmod +x "$dir/terraform-provider-${type}_v$version"
done
