#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(
    cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &&
    pwd
)"

OLD_REPO_1='HeimdallStudio/Heimdall''-Panel'
OLD_REPO_2='PasarGuard/''panel'
OLD_REPO_3='mmdgogli/''Nini-Kocholo'

FORBIDDEN="${OLD_REPO_1}|${OLD_REPO_2}|${OLD_REPO_3}" 

echo "Checking all tracked source repository routing..."

set +e
MATCHES="$(
    cd "$ROOT" &&
    git grep -n -I -E "$FORBIDDEN" \
        -- . \
        ':(exclude)internal/web/dist/**' \
        2>/dev/null
)"
RC=$?
set -e

if [ "$RC" -eq 0 ] && [ -n "$MATCHES" ]; then
    echo
    echo "ERROR: forbidden repository references found:"
    echo "$MATCHES" | head -100
    exit 1
fi

if [ "$RC" -ne 0 ] && [ "$RC" -ne 1 ]; then
    echo "ERROR: git grep failed with code $RC"
    exit "$RC"
fi


require_string() {
    local file="$1"
    local value="$2"

    grep -Fq "$value" "$file" || {
        echo
        echo "ERROR: expected Nini-Kocholo reference missing:"
        echo "File: $file"
        echo "Expected: $value"
        exit 1
    }
}


require_string \
    "$ROOT/install.sh" \
    "api.github.com/repos/mmdgogoli/Nini-Kocholo/releases/latest"

require_string \
    "$ROOT/update.sh" \
    "api.github.com/repos/mmdgogoli/Nini-Kocholo/releases/latest"

require_string \
    "$ROOT/internal/web/service/panel/panel.go" \
    "api.github.com/repos/mmdgogoli/Nini-Kocholo/releases/latest"

require_string \
    "$ROOT/internal/web/service/panel/panel.go" \
    "raw.githubusercontent.com/mmdgogoli/Nini-Kocholo/main/update.sh"

require_string \
    "$ROOT/frontend/src/pg-ui/hooks/use-version-check.ts" \
    "api.github.com/repos/mmdgogoli/Nini-Kocholo/releases/latest"

require_string \
    "$ROOT/frontend/src/pg-ui/constants/Project.ts" \
    "https://github.com/mmdgogoli/Nini-Kocholo"

require_string \
    "$ROOT/frontend/src/layouts/AppSidebar.tsx" \
    "https://github.com/mmdgogoli/Nini-Kocholo"

echo "All source repository routing: PASS"
