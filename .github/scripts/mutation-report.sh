#!/usr/bin/env bash
# Diff-scoped mutation report for dogfooding.
#
# Runs gremlins on the Go packages changed in this PR (one at a time — gremlins
# takes a single package pattern, and a whole-repo `./...` run trips over the
# e2e package's binary-building tests), then merges the per-package JSON reports
# into a single gremlins.json at the repo root. Each report's file_name is
# rewritten from a package-local basename (e.g. scorer.go) to a repo-relative
# path (internal/scorer/scorer.go) so `ods check --mutation` can match it
# against the diff unambiguously.
#
# Advisory and best-effort: any gremlins failure/timeout is logged and skipped;
# the script always leaves a valid gremlins.json (possibly {"files":[]}) and
# exits 0 so it never blocks the gate.
#
# Usage: mutation-report.sh <base-ref> [output-file]
set -uo pipefail

BASE="${1:-origin/main}"
OUT="${2:-gremlins.json}"
PER_PKG_TIMEOUT="${MUTATION_PKG_TIMEOUT:-300}"

export PATH="$PATH:$(go env GOPATH)/bin"

# Always leave a valid (empty) report so downstream never sees a missing file.
echo '{"files":[]}' > "$OUT"

if ! command -v gremlins >/dev/null 2>&1; then
  echo "Installing gremlins..."
  go install github.com/go-gremlins/gremlins/cmd/gremlins@latest || {
    echo "::warning::could not install gremlins — skipping mutation report"
    exit 0
  }
fi

# Package dirs of changed .go files, minus the e2e package (its tests build
# binaries / spawn subprocesses — far too slow to mutate).
mapfile -t DIRS < <(git diff --name-only "$BASE"...HEAD -- '*.go' \
  | xargs -r -n1 dirname | sort -u)

TMP="$(mktemp -d)"
IDX=0
RAN=0
for d in "${DIRS[@]}"; do
  [ "$d" = "internal/e2e" ] && continue
  # gremlins needs a test suite in the package, and the dir must still exist.
  ls "$d"/*_test.go >/dev/null 2>&1 || continue
  echo "::group::gremlins $d"
  if timeout "$PER_PKG_TIMEOUT" gremlins unleash --output "$TMP/g-$IDX.json" "./$d/"; then
    echo "$d" > "$TMP/dir-$IDX.txt"
    RAN=$((RAN + 1))
  else
    echo "::warning::gremlins failed or timed out on $d — skipping"
  fi
  echo "::endgroup::"
  IDX=$((IDX + 1))
done

if [ "$RAN" -eq 0 ]; then
  echo "No changed Go packages to mutate — leaving empty report."
  exit 0
fi

# Merge, rewriting file_name to a repo-relative path.
python3 - "$TMP" "$OUT" <<'PY'
import glob, json, os, sys
tmp, out = sys.argv[1], sys.argv[2]
files = []
for gp in sorted(glob.glob(os.path.join(tmp, "g-*.json"))):
    idx = os.path.basename(gp)[2:-5]  # g-<idx>.json
    dtxt = os.path.join(tmp, f"dir-{idx}.txt")
    if not os.path.exists(dtxt):
        continue
    pkgdir = open(dtxt).read().strip()
    try:
        rep = json.load(open(gp))
    except Exception:
        continue
    for f in rep.get("files") or []:
        f["file_name"] = f"{pkgdir}/{os.path.basename(f.get('file_name', ''))}"
        files.append(f)
json.dump({"files": files}, open(out, "w"))
print(f"Merged {len(files)} mutated file(s) into {out}")
PY
