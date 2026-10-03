#!/usr/bin/env bash
set -euo pipefail
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
script="$(pwd)/utils/headless/upstream-release.sh"
export REAL_GIT
REAL_GIT=$(command -v git)
export MOCK_ROOT="$stage" GITHUB_REPOSITORY=example/fork
mkdir "$stage/bin"
cat > "$stage/bin/git" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
args=()
for arg in "$@"; do
    case "$arg" in
        https://github.com/ProtonMail/proton-bridge.git) arg="$MOCK_ROOT/upstream";;
        https://github.com/example/fork.git) arg="$MOCK_ROOT/origin";;
    esac
    args+=("$arg")
done
exec "$REAL_GIT" "${args[@]}"
MOCK
cat > "$stage/bin/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
    'api repos/ProtonMail/proton-bridge/releases/latest') cat "$MOCK_ROOT/release.json";;
    'api --paginate repos/example/fork/pulls?'*)
        if [[ ${MOCK_PR_FAILURE:-false} == true ]]; then exit 17; fi
        cat "$MOCK_ROOT/prs.json";;
    'pr create '*)
        printf '%s\n' "$*" > "$MOCK_ROOT/created-pr"
        while (( $# )); do
            if [[ $1 == --body-file ]]; then cp "$2" "$MOCK_ROOT/pr-body"; break; fi
            shift
        done;;
    *) echo "Unexpected gh call: $*" >&2; exit 1;;
esac
MOCK
chmod +x "$stage/bin/"*
export PATH="$stage/bin:$PATH"
export GIT_AUTHOR_NAME=Test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=Test GIT_COMMITTER_EMAIL=test@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

fixture() {
    local policy_path=${POLICY_PATH:-.github/policy.yml}
    cd "$stage"
    rm -rf upstream fork origin state created-pr pr-body
    git init -q -b main upstream
    cd upstream
    cat > Makefile <<'MAKE'
BRIDGE_APP_VERSION?=1.0.0+git

# Enough context to keep the version bump separate from fork targets.
build:
	@echo upstream

other:
	@echo upstream

last:
	@echo upstream
MAKE
    printf 'original\n' > mail.txt
    printf 'original\n' > second.txt
    mkdir -p "$(dirname "$policy_path")"
    printf 'original\n' > "$policy_path"
    git add .
    git commit -qm initial
    git tag v1.0.0
    git clone -q . "$stage/fork"
    cd "$stage/fork"
    mkdir -p utils/headless
    for target in build check smoke; do touch "utils/headless/$target.sh"; done
    cat >> Makefile <<'MAKE'

headless-build:
	./utils/headless/run.sh build
headless-check:
	./utils/headless/run.sh check
headless-smoke:
	./utils/headless/smoke.sh
MAKE
    printf 'fork behavior\n' > fork.txt
    if [[ $1 == conflict ]]; then
        printf 'fork behavior\n' > mail.txt
        printf 'fork behavior\n' > second.txt
    elif [[ $1 == policy ]]; then
        printf 'fork policy\n' > "$policy_path"
    fi
    git add .
    git commit -qm fork
    export BASE_SHA
    BASE_SHA=$(git rev-parse HEAD)
    git clone -q --bare . "$stage/origin"
    cd "$stage/upstream"
    sed -i 's/1.0.0/1.1.0/' Makefile
    if [[ $1 == conflict ]]; then
        printf 'upstream behavior\n' > mail.txt
        printf 'upstream behavior\n' > second.txt
    elif [[ $1 == policy ]]; then
        printf 'upstream policy\n' > "$policy_path"
    fi
    git add .
    git commit -qm release
    git tag v1.1.0
    export UPSTREAM_SHA RELEASE_TAG=v1.1.0 SYNC_BRANCH=bot/upstream-release/v1.1.0
    export RELEASE_URL=https://github.com/ProtonMail/proton-bridge/releases/tag/v1.1.0
    UPSTREAM_SHA=$(git rev-parse HEAD)
    jq -n --arg tag "$RELEASE_TAG" --arg url "$RELEASE_URL" \
        '{tag_name:$tag,html_url:$url,draft:false,prerelease:false}' > "$stage/release.json"
    echo '[]' > "$stage/prs.json"
    export SYNC_STATE="$stage/state" GITHUB_OUTPUT="$stage/output"
    : > "$GITHUB_OUTPUT"
    cd "$stage/fork"
}

run() { bash "$script" "$@"; }
reject() {
    if run "$1" > "$stage/rejected.log" 2>&1; then echo "Unexpected success: $*" >&2; exit 1; fi
    grep -Fq "$2" "$stage/rejected.log"
    test ! -e "$stage/created-pr"
}

fixture clean
for flag in draft prerelease; do
    jq --arg flag "$flag" '.[$flag] = true' "$stage/release.json" > "$stage/rejected-release.json"
    cp "$stage/release.json" "$stage/stable.json"
    cp "$stage/rejected-release.json" "$stage/release.json"
    run detect
    grep -Fxq ready=false "$GITHUB_OUTPUT"
    test ! -e "$SYNC_STATE/open-prs.json"
    cp "$stage/stable.json" "$stage/release.json"
done
# Bootstrap never downgrades: an older tag already in main produces no PR.
jq '.tag_name="v1.0.0" | .html_url="https://github.com/ProtonMail/proton-bridge/releases/tag/v1.0.0"' \
    "$stage/release.json" > "$stage/old.json"
cp "$stage/old.json" "$stage/release.json"
run detect
test ! -e "$SYNC_STATE/open-prs.json"

fixture clean
run detect
grep -Fxq ready=true "$GITHUB_OUTPUT"
run verify
grep -Fxq conflicts=false "$GITHUB_OUTPUT"
test "$(git show -s --format=%P HEAD)" = "$BASE_SHA $UPSTREAM_SHA"
git merge-base --is-ancestor "$UPSTREAM_SHA" HEAD
grep -Fxq 'fork behavior' fork.txt
run publish
grep -Fq -- '--base main --head bot/upstream-release/v1.1.0' "$stage/created-pr"
grep -Fq "$RELEASE_URL" "$stage/pr-body"
grep -Fq "$BASE_SHA" "$stage/pr-body"
grep -Fq "$UPSTREAM_SHA" "$stage/pr-body"
grep -Fq 'Codex was not invoked' "$stage/pr-body"

# An open PR suppresses detection and also the final publishing step.
rm "$stage/created-pr"
jq -n --arg branch "$SYNC_BRANCH" '[{head:{ref:$branch},body:""}]' > "$stage/prs.json"
run publish
test ! -e "$stage/created-pr"
git checkout -q --detach "$BASE_SHA"
: > "$GITHUB_OUTPUT"
run detect
grep -Fxq ready=false "$GITHUB_OUTPUT"
test "$(git rev-parse HEAD)" = "$BASE_SHA"

# A renamed tag with the same SHA and a newer open PR's ancestry also suppress it.
jq -n --arg sha "$UPSTREAM_SHA" '[{head:{ref:"bot/upstream-release/v1.2.0"},body:("<!-- upstream-release:"+$sha+" -->")}]' > "$stage/prs.json"
run detect
jq -n --arg sha "$(git --git-dir="$stage/origin" rev-parse refs/heads/bot/upstream-release/v1.1.0)" \
    '[{head:{ref:"bot/upstream-release/v1.2.0",sha:$sha,repo:{full_name:"example/fork"}},body:""}]' > "$stage/prs.json"
run detect
export MOCK_PR_FAILURE=true
reject detect 'Could not query open PRs'
unset MOCK_PR_FAILURE

fixture conflict
run detect
run merge
grep -Fxq conflicts=true "$GITHUB_OUTPUT"
printf 'fork and upstream behavior\n' > mail.txt
printf 'fork and upstream behavior\n' > second.txt
printf 'Preserved both behaviors.\n' > "$SYNC_STATE/codex-summary.md"
run package
git merge --abort
run verify
test "$(git show -s --format=%P HEAD)" = "$BASE_SHA $UPSTREAM_SHA"
grep -Fxq 'fork and upstream behavior' mail.txt
run publish
grep -Fq 'Preserved both behaviors.' "$stage/pr-body"

fixture conflict
run merge
reject package 'Agent left conflicts unresolved'

# A partial resolution still leaves a marker-bearing tree and cannot open a PR.
printf 'resolved\n' > mail.txt
run package
git merge --abort
reject verify 'Unresolved conflict markers'

fixture conflict
run merge
printf 'resolved\n' > mail.txt
printf 'resolved\n' > second.txt
printf 'unrelated agent change\n' > fork.txt
reject package 'Out-of-scope agent change: fork.txt'
# The fresh verifier also rejects a forged patch that bypasses agent packaging.
git diff --binary "$(cat "$SYNC_STATE/automatic-tree")" > "$SYNC_STATE/resolution.patch"
git merge --abort
reject verify 'Out-of-scope agent change: fork.txt'

for POLICY_PATH in .github/policy.yml .gitlab-ci.yml .gitlab/CODEOWNERS .githooks/pre-push .grype.yaml; do
    export POLICY_PATH
    fixture policy
    reject merge 'Manual repair required'
done
unset POLICY_PATH

fixture clean
sed -i 's/1.1.0/0.9.0/' "$stage/upstream/Makefile"
git -C "$stage/upstream" commit -qam 'wrong version'
UPSTREAM_SHA=$(git -C "$stage/upstream" rev-parse HEAD)
reject verify 'does not match v1.1.0'

echo 'Stable upstream release integration checks passed'
