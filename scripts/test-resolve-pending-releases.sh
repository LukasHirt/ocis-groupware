#!/usr/bin/env bash
# Tests for resolve-pending-releases.sh. No network: every upstream and Docker
# Hub input is supplied through the resolver's test hooks, and tag
# verification is replaced by a stub. Run it directly; CI runs it in the
# `test` job of .github/workflows/main.yml.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESOLVER="${SCRIPT_DIR}/resolve-pending-releases.sh"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Verification stub: prints a fake commit SHA for the tag, and fails (like an
# unsigned tag) for every tag listed in $UNVERIFIED.
STUB="${TMP}/verify-tag"
cat >"$STUB" <<'EOF'
#!/usr/bin/env bash
for t in ${UNVERIFIED:-}; do
  if [ "$t" = "$1" ]; then
    echo "::warning::$1 is not verified (stub)" >&2
    exit 1
  fi
done
echo "sha-of-$1"
EOF
chmod +x "$STUB"

failures=0

lines() { [ "$#" -eq 0 ] && return 0; printf '%s\n' "$@"; }

# expect <name> <mode> <upstream tags> <published tags> <unverified> <expected json>
# Tag lists are space-separated.
expect() {
  local name=$1 mode=$2 upstream=$3 published=$4 unverified=$5 expected=$6 actual
  # shellcheck disable=SC2086 # word splitting of the tag lists is intended
  if ! actual=$(MODE="$mode" \
    UPSTREAM_TAGS="$(lines $upstream)" \
    PUBLISHED_TAGS="$(lines $published)" \
    UPSTREAM_MAIN_SHA=mainsha \
    UNVERIFIED="$unverified" \
    VERIFY_TAG="$STUB" \
    "$RESOLVER" 2>"${TMP}/stderr"); then
    echo "FAIL ${name}: resolver exited non-zero"
    sed 's/^/  /' "${TMP}/stderr"
    failures=$((failures + 1))
    return
  fi
  if [ "$(jq -S -c 'sort_by(.version)' <<<"$actual")" = "$(jq -S -c 'sort_by(.version)' <<<"$expected")" ]; then
    echo "ok ${name}"
  else
    echo "FAIL ${name}"
    echo "  expected: ${expected}"
    echo "  actual:   ${actual}"
    failures=$((failures + 1))
  fi
}

entry() { # entry <version> <latest>
  printf '{"version":"%s","ref":"v%s","sha":"sha-of-v%s","latest":%s}' "$1" "$1" "$1" "$2"
}

expect no_tags_pending_empty pending "" "" "" '[]'

expect pr_mode_no_tags_builds_main pr "" "" "" \
  '[{"version":"main","ref":"main","sha":"mainsha","latest":false}]'

expect two_pending_both_built pending "v0.1.0 v0.2.0" "" "" \
  "[$(entry 0.1.0 false),$(entry 0.2.0 true)]"

expect latest_uses_version_sort pending "v0.9.0 v0.10.0" "" "" \
  "[$(entry 0.9.0 false),$(entry 0.10.0 true)]"

expect published_skipped pending "v0.1.0 v0.2.0" "0.1.0 0.1.0-20261101 latest" "" \
  "[$(entry 0.2.0 true)]"

expect older_missing_not_latest pending "v0.1.0 v0.2.0" "0.2.0 latest" "" \
  "[$(entry 0.1.0 false)]"

expect all_published_pending_empty pending "v0.1.0 v0.2.0" "0.1.0 0.2.0" "" '[]'

expect rebuild_adds_newest rebuild "v0.1.0 v0.2.0" "0.1.0 0.2.0" "" \
  "[$(entry 0.2.0 true)]"

expect rebuild_keeps_pending rebuild "v0.1.0 v0.2.0" "0.2.0" "" \
  "[$(entry 0.1.0 false),$(entry 0.2.0 true)]"

expect pr_mode_tags_builds_newest_only pr "v0.1.0 v0.2.0" "0.1.0 0.2.0" "" \
  "[$(entry 0.2.0 true)]"

expect non_plain_tags_ignored pending "v0.1.0 v0.2.0-rc.1 v1.0 foo 0.3.0 v0.4.0^{}" "" "" \
  "[$(entry 0.1.0 true)]"

expect unverified_tag_skipped pending "v0.1.0 v0.2.0" "" "v0.2.0" \
  "[$(entry 0.1.0 false)]"
if grep -q '::warning::' "${TMP}/stderr"; then
  echo "ok unverified_tag_skipped_warns"
else
  echo "FAIL unverified_tag_skipped_warns: no ::warning:: on stderr"
  failures=$((failures + 1))
fi

# Docker Hub unreachable: the resolver must fail rather than report nothing
# as published (which would rebuild everything) or emit an empty matrix.
if out=$(MODE=pending UPSTREAM_TAGS=v0.1.0 UPSTREAM_MAIN_SHA=mainsha VERIFY_TAG="$STUB" \
  DOCKERHUB_TAGS_URL=http://127.0.0.1:9/ CURL_RETRY=0 \
  "$RESOLVER" 2>"${TMP}/stderr"); then
  echo "FAIL hub_unreachable_fails: resolver succeeded with: ${out}"
  failures=$((failures + 1))
elif [ -n "$out" ]; then
  echo "FAIL hub_unreachable_fails: resolver printed: ${out}"
  failures=$((failures + 1))
else
  echo "ok hub_unreachable_fails"
fi

if [ "$failures" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi
echo "all tests passed"
