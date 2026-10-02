#!/usr/bin/env bash
set -euo pipefail

# This script must come from main, never from the merged tree or agent output.
: "${SYNC_STATE:?Use a directory outside the checkout}"
mkdir -p "$SYNC_STATE"
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null

fail() { echo "$*" >&2; exit 1; }
output() { printf '%s=%s\n' "$1" "$2" >> "${GITHUB_OUTPUT:?}"; }

covered() {
    gh api --paginate "repos/$GITHUB_REPOSITORY/pulls?state=open&base=main&per_page=100" > "$SYNC_STATE/prs.json" || fail 'Could not query open PRs'
    jq -s 'add // []' "$SYNC_STATE/prs.json" > "$SYNC_STATE/open-prs.json" || fail 'Invalid open PR response'
    local duplicate
    duplicate=$(jq -r --arg branch "$SYNC_BRANCH" --arg marker "<!-- upstream-release:$UPSTREAM_SHA -->" '
        any(.[]; (.head.ref | startswith("bot/upstream-release/")) and
            (.head.ref == $branch or ((.body // "") | contains($marker))))
        ' "$SYNC_STATE/open-prs.json") || fail 'Invalid open PR response'
    if [[ $duplicate == true ]]; then
        echo 'An open release PR already covers this release'
        return 0
    fi
    # A newer open release PR can already contain this tag too.
    while read -r sha; do
        git fetch --no-tags "https://github.com/$GITHUB_REPOSITORY.git" "$sha" || fail 'Could not inspect open PR ancestry'
        if git merge-base --is-ancestor "$UPSTREAM_SHA" FETCH_HEAD; then
            echo 'An open release PR already contains this release'
            return 0
        fi
    done < <(jq -r '.[] | select(.head.repo.full_name == env.GITHUB_REPOSITORY) |
        select(.head.ref | startswith("bot/upstream-release/")) | .head.sha' "$SYNC_STATE/open-prs.json")
    return 1
}

detect() {
    output ready false
    gh api repos/ProtonMail/proton-bridge/releases/latest > "$SYNC_STATE/release.json"
    if ! jq -e '.draft == false and .prerelease == false' "$SYNC_STATE/release.json" >/dev/null; then
        echo 'No stable release candidate'
        return
    fi
    RELEASE_TAG=$(jq -er .tag_name "$SYNC_STATE/release.json")
    [[ $RELEASE_TAG =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "Not a stable version tag: $RELEASE_TAG"
    RELEASE_URL=$(jq -er .html_url "$SYNC_STATE/release.json")
    [[ $RELEASE_URL == "https://github.com/ProtonMail/proton-bridge/releases/tag/$RELEASE_TAG" ]] || fail 'Unexpected release URL'
    git fetch --no-tags https://github.com/ProtonMail/proton-bridge.git \
        "refs/tags/$RELEASE_TAG:refs/upstream-sync/release"
    UPSTREAM_SHA=$(git rev-parse 'refs/upstream-sync/release^{commit}')
    BASE_SHA=$(git rev-parse HEAD)
    if git merge-base --is-ancestor "$UPSTREAM_SHA" "$BASE_SHA"; then
        echo "$RELEASE_TAG is already integrated"
        return
    fi
    SYNC_BRANCH="bot/upstream-release/$RELEASE_TAG"
    if covered; then return; fi
    output base_sha "$BASE_SHA"
    output upstream_sha "$UPSTREAM_SHA"
    output tag "$RELEASE_TAG"
    output release_url "$RELEASE_URL"
    output branch "$SYNC_BRANCH"
    output ready true
}

merge_release() {
    [[ ${BASE_SHA:-} =~ ^[0-9a-f]{40}$ && ${UPSTREAM_SHA:-} =~ ^[0-9a-f]{40}$ ]] || fail 'Invalid pinned commits'
    git fetch --no-tags https://github.com/ProtonMail/proton-bridge.git "$UPSTREAM_SHA"
    git checkout --detach "$BASE_SHA"
    git config user.name 'github-actions[bot]'
    git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
    local status=0 path
    git merge --no-ff --no-commit "$UPSTREAM_SHA" > "$SYNC_STATE/merge.log" 2>&1 || status=$?
    cat "$SYNC_STATE/merge.log"
    git diff --name-only --diff-filter=U -z > "$SYNC_STATE/conflicts"
    git diff --cc > "$SYNC_STATE/conflicts.patch"
    if (( status != 0 )) && [[ ! -s $SYNC_STATE/conflicts ]]; then fail 'Merge failed without resolvable conflicts'; fi
    git rev-parse --verify MERGE_HEAD >/dev/null || fail 'No merge to propose'
    while IFS= read -r -d '' path; do
        case "$path" in
            .github/*|.gitlab/*|.gitlab-ci.yml|.githooks/*|.codex/*|.agents/*|scripts/*|utils/*|ci/*|AGENTS*.md|*/AGENTS*.md|SECURITY*|*/SECURITY*|.gitmodules|.gitattributes|.grype.yaml|.golangci.yml|.dockerignore|*Dockerfile*|*security-policy*)
                fail "Manual repair required for automation or security-policy conflict: $path";;
        esac
    done < "$SYNC_STATE/conflicts"
    # Save Git's automatic merge, with markers for unresolved paths. Restore the
    # unmerged index so Codex sees the actual conflict stages.
    cp "$(git rev-parse --git-path index)" "$SYNC_STATE/index"
    git add --all
    git write-tree > "$SYNC_STATE/automatic-tree"
    cp "$SYNC_STATE/index" "$(git rev-parse --git-path index)"
    if [[ -s $SYNC_STATE/conflicts ]]; then output conflicts true; else output conflicts false; fi
}

validate_resolution() {
    local tree=$1 path automatic
    automatic=$(cat "$SYNC_STATE/automatic-tree")
    git diff --no-renames --name-only -z "$automatic" "$tree" > "$SYNC_STATE/changed-paths"
    while IFS= read -r -d '' path; do
        grep -zFxq -- "$path" "$SYNC_STATE/conflicts" || fail "Out-of-scope agent change: $path"
    done < "$SYNC_STATE/changed-paths"
    git diff --check "$automatic" "$tree"
}

validate_version() {
    local tree=$1 version target
    git show "$tree:Makefile" > "$SYNC_STATE/Makefile"
    version=$(sed -n 's/^BRIDGE_APP_VERSION?=\([0-9.]*\)+git$/\1/p' "$SYNC_STATE/Makefile")
    [[ $version == "${RELEASE_TAG#v}" ]] || fail "Makefile version $version does not match $RELEASE_TAG"
    for target in build check smoke; do
        grep -Fxq "headless-$target:" "$SYNC_STATE/Makefile" || fail "Missing headless-$target target"
        git cat-file -e "$tree:scripts/headless/$target.sh" || fail "Missing fork $target script"
    done
    grep -Fxq $'\t./scripts/headless/run.sh build' "$SYNC_STATE/Makefile" || fail 'Changed headless build recipe'
    grep -Fxq $'\t./scripts/headless/run.sh check' "$SYNC_STATE/Makefile" || fail 'Changed headless check recipe'
    grep -Fxq $'\t./scripts/headless/smoke.sh' "$SYNC_STATE/Makefile" || fail 'Changed headless smoke recipe'
}

package_resolution() {
    [[ $(git rev-parse HEAD) == "$BASE_SHA" ]] || fail 'Agent changed HEAD'
    [[ $(git rev-parse MERGE_HEAD) == "$UPSTREAM_SHA" ]] || fail 'Agent changed merge parent'
    git add --all
    local tree
    tree=$(git write-tree)
    validate_resolution "$tree"
    validate_version "$tree"
    git diff --binary --no-ext-diff --no-textconv "$(cat "$SYNC_STATE/automatic-tree")" "$tree" > "$SYNC_STATE/resolution.patch"
    [[ -s $SYNC_STATE/resolution.patch ]] || fail 'Agent left conflicts unresolved'
}

verify() {
    merge_release
    local automatic tree
    automatic=$(cat "$SYNC_STATE/automatic-tree")
    if [[ -s $SYNC_STATE/conflicts ]]; then
        [[ -s $SYNC_STATE/resolution.patch ]] || fail 'Missing conflict resolution'
        git read-tree --reset -u "$automatic"
        git apply --index --binary "$SYNC_STATE/resolution.patch"
    else
        [[ ! -s $SYNC_STATE/resolution.patch ]] || fail 'Unexpected resolution for a clean merge'
    fi
    tree=$(git write-tree)
    validate_resolution "$tree"
    validate_version "$tree"
    # Check the entire tree: a partial repair must not leave markers in another
    # conflicted file, even if that file was absent from the agent's patch.
    local paths=()
    mapfile -d '' -t paths < "$SYNC_STATE/conflicts"
    if (( ${#paths[@]} )); then
        local status=0
        git grep -n -E '^(<<<<<<< |=======\r?$|>>>>>>> )' "$tree" -- "${paths[@]}" > "$SYNC_STATE/markers.log" || status=$?
        [[ $status == 1 ]] || fail 'Unresolved conflict markers (see markers.log)'
    fi
    git -c commit.gpgsign=false commit --no-verify -m "Merge upstream release $RELEASE_TAG"
    [[ $(git show -s --format=%P HEAD) == "$BASE_SHA $UPSTREAM_SHA" ]] || fail 'Invalid merge ancestry'
    git merge-base --is-ancestor "$UPSTREAM_SHA" HEAD || fail 'Release ancestry lost'
    git rev-parse HEAD > "$SYNC_STATE/candidate-sha"
}

# shellcheck disable=SC2016 # Markdown backticks are literal.
publish() {
    [[ $RELEASE_TAG =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'Invalid release tag'
    [[ $SYNC_BRANCH == "bot/upstream-release/$RELEASE_TAG" ]] || fail 'Invalid branch'
    [[ $(git rev-parse HEAD) == "$(cat "$SYNC_STATE/candidate-sha")" ]] || fail 'Candidate changed after verification'
    git fetch --no-tags "https://github.com/$GITHUB_REPOSITORY.git" refs/heads/main
    if git merge-base --is-ancestor "$UPSTREAM_SHA" FETCH_HEAD; then echo 'Release already integrated'; return; fi
    [[ $(git rev-parse FETCH_HEAD) == "$BASE_SHA" ]] || fail 'main advanced; retry from its new tip next run'
    if covered; then return; fi
    local old body
    old=$(git ls-remote "https://github.com/$GITHUB_REPOSITORY.git" "refs/heads/$SYNC_BRANCH" | cut -f1)
    git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push --no-verify \
        "--force-with-lease=refs/heads/$SYNC_BRANCH:$old" \
        "https://github.com/$GITHUB_REPOSITORY.git" "HEAD:refs/heads/$SYNC_BRANCH"
    body="$SYNC_STATE/pr-body.md"
    {
        printf 'Merge stable upstream release [%s](%s) into `main`.\n\n' "$RELEASE_TAG" "$RELEASE_URL"
        printf 'Pinned main commit: `%s`\n\nPinned upstream tag commit: `%s`\n\nMerge commit: `%s`\n\n' \
            "$BASE_SHA" "$UPSTREAM_SHA" "$(git rev-parse HEAD)"
        if [[ -s $SYNC_STATE/conflicts ]]; then
            printf 'Codex resolved only these conflicted paths:\n\n'
            while IFS= read -r -d '' path; do printf -- '- `%s`\n' "$path"; done < "$SYNC_STATE/conflicts"
            printf '\nAgent conflict-resolution summary (requires human review):\n\n'
            cat "$SYNC_STATE/codex-summary.md"
        else
            printf 'Git merged the release cleanly; Codex was not invoked.\n'
        fi
        printf '\nApprove only after existing headless, race, vulnerability, image and runtime smoke checks succeed. Merge using **Create a merge commit**, then delete the temporary branch.\n\n'
        printf '<!-- upstream-release:%s -->\n' "$UPSTREAM_SHA"
    } > "$body"
    gh pr create --repo "$GITHUB_REPOSITORY" --base main --head "$SYNC_BRANCH" \
        --title "Merge upstream stable release $RELEASE_TAG" --body-file "$body"
}

case "${1:-}" in
    detect) detect;;
    merge) merge_release;;
    package) package_resolution;;
    verify) verify;;
    publish) publish;;
    *) fail 'Usage: upstream-release.sh detect|merge|package|verify|publish';;
esac
