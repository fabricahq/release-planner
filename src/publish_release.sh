#!/usr/bin/env bash
# Tag the approved commit and publish the approved notes as a GitHub release.
# Reads the plan printed by `release_planner.py plan`. Every remote fact is verified
# before and after writing; a retry after a successful publication makes no writes.
# Requires gh and jq, plus GH_TOKEN with contents: write and GH_REPO (owner/name).
set -euo pipefail

plan=${1:?usage: publish_release.sh <release-plan.json>}
: "${GH_REPO:?set GH_REPO to owner/name}"
notes=$(mktemp)
trap 'rm -f "$notes"' EXIT
tag=$(jq -r .tag "$plan")
commit=$(jq -r .commit "$plan")
previous=$(jq -r .previous "$plan")
prerelease=$(jq -r .prerelease "$plan")
jq -j .notes "$plan" > "$notes"
[ -n "$tag" ] || { echo 'The plan requests no release'; exit 1; }
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || { echo 'The plan has no approved commit'; exit 1; }

# Resolve a remote tag to its commit, peeling annotated tags. Prints nothing if absent.
tag_commit() {
  local ref
  ref=$(gh api "repos/$GH_REPO/git/ref/tags/$1" 2>/dev/null) || return 0
  if [ "$(jq -r .object.type <<<"$ref")" = tag ]; then
    gh api "repos/$GH_REPO/git/tags/$(jq -r .object.sha <<<"$ref")" -q .object.sha
  else
    jq -r .object.sha <<<"$ref"
  fi
}

# Refuse to publish over version tags that appeared after planning.
semver='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
remote=$(gh api --paginate "repos/$GH_REPO/git/matching-refs/tags/v" -q '.[].ref' | sed 's#^refs/tags/##' | grep -E "$semver" | grep -vxF "$tag" | sort || true)
[ "$remote" = "$(jq -r '.tags[]' "$plan" | sort)" ] || { echo 'Version tags changed since planning; plan again'; exit 1; }

if [ -n "$previous" ]; then
  [ "$(gh release view "$previous" --json isDraft -q .isDraft)" = false ] || { echo "Publish $previous first"; exit 1; }
fi

existing=$(tag_commit "$tag")
if [ -n "$existing" ] && [ "$existing" != "$commit" ]; then
  echo "$tag already points to $existing, not the approved $commit"; exit 1
fi

latest=true
[ "$prerelease" = true ] && latest=false
if release=$(gh release view "$tag" --json name,body,isDraft,isPrerelease 2>/dev/null); then
  jq -e --arg tag "$tag" --argjson pre "$prerelease" --rawfile notes "$notes" \
    'def norm: gsub("\r\n"; "\n") | sub("\\s+$"; ""); .name == $tag and (.body | norm) == ($notes | norm) and .isPrerelease == $pre' \
    <<<"$release" > /dev/null || { echo "An existing $tag release differs from the approved notes; resolve it by hand"; exit 1; }
  if [ "$(jq -r .isDraft <<<"$release")" = false ]; then
    echo "$tag is already published"
  else
    gh release edit "$tag" --draft=false --latest="$latest"
  fi
else
  flags=(--latest="$latest")
  [ "$prerelease" = true ] && flags+=(--prerelease)
  gh release create "$tag" --target "$commit" --title "$tag" --notes-file "$notes" "${flags[@]}"
fi

[ "$(tag_commit "$tag")" = "$commit" ] || { echo "$tag does not point to the approved commit after publication"; exit 1; }
gh release view "$tag" --json url -q .url
