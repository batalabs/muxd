#!/bin/bash
# Bump MuxdMobile MARKETING_VERSION and CURRENT_PROJECT_VERSION.
# Usage:
#   ./bump-version.sh patch   # 1.7.1 → 1.7.2, build++
#   ./bump-version.sh minor   # 1.7.1 → 1.8.0, build++
#   ./bump-version.sh major   # 1.7.1 → 2.0.0, build++
#   ./bump-version.sh 2.0.0   # set exact version, build++
#   ./bump-version.sh build   # keep version, build++ only

set -euo pipefail

PBXPROJ="MuxdMobile/MuxdMobile.xcodeproj/project.pbxproj"

if [ ! -f "$PBXPROJ" ]; then
    echo "error: $PBXPROJ not found (run from MuxdMobile/)" >&2
    exit 1
fi

# Read current marketing version
CURRENT=$(grep 'MARKETING_VERSION' "$PBXPROJ" | head -1 | sed 's/.*= *//;s/;.*//')
if [ -z "$CURRENT" ]; then
    echo "error: MARKETING_VERSION not found in $PBXPROJ" >&2
    exit 1
fi

# Read current build number
BUILD=$(grep 'CURRENT_PROJECT_VERSION' "$PBXPROJ" | head -1 | sed 's/.*= *//;s/;.*//')
BUILD=${BUILD:-0}
NEW_BUILD=$((BUILD + 1))

IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT"

case "${1:-patch}" in
    major)  NEW="$((MAJOR + 1)).0.0" ;;
    minor)  NEW="${MAJOR}.$((MINOR + 1)).0" ;;
    patch)  NEW="${MAJOR}.${MINOR}.$((PATCH + 1))" ;;
    build)  NEW="$CURRENT" ;;
    [0-9]*) NEW="$1" ;;
    *)      echo "usage: $0 [major|minor|patch|build|X.Y.Z]" >&2; exit 1 ;;
esac

# Update both values in pbxproj (appears twice: Debug + Release)
sed -i '' "s/MARKETING_VERSION = ${CURRENT}/MARKETING_VERSION = ${NEW}/g" "$PBXPROJ"
sed -i '' "s/CURRENT_PROJECT_VERSION = ${BUILD}/CURRENT_PROJECT_VERSION = ${NEW_BUILD}/g" "$PBXPROJ"

echo "${CURRENT} (${BUILD}) → ${NEW} (${NEW_BUILD})"
