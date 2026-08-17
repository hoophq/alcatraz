#!/usr/bin/env bash
#
# Mirror a pinned model into an S3 bucket laid out like the hub, then prove the
# result by downloading it back through the origin an operator would use.
#
# The file list, digests and keys all come from "alcatraz models pins", so this
# script never carries a second copy of the pin table.
#
#   hack/mirror-model.sh --bucket s3://hoop-models/alcatraz \
#                        --origin https://models.hoop.dev/alcatraz
#
# Credentials come from the ambient AWS config; none belong in this repository.
# In CI, assume a role via OIDC rather than issuing a long-lived key with write
# access to a public bucket.
set -euo pipefail

usage() {
	cat <<'EOF'
usage: hack/mirror-model.sh --bucket s3://bucket/prefix --origin https://host/prefix [options]

  --bucket   s3 destination, optionally with a key prefix (required)
  --origin   public base URL the bucket is served under. Given, the upload is
             followed by a download through it; omitted, that check is skipped
  --model    model id to mirror (default: the pinned default)
  --dry-run  print what would be uploaded and exit
  --force    overwrite keys that already exist

Uploads are write-once by default: a revision already present is left alone,
because a pinned revision that changes bytes is a mistake, not an update.
EOF
}

bucket=""
origin=""
model=""
dry_run=false
force=false

while [ $# -gt 0 ]; do
	case "$1" in
	--bucket) bucket="${2:-}"; shift 2 ;;
	--origin) origin="${2:-}"; shift 2 ;;
	--model) model="${2:-}"; shift 2 ;;
	--dry-run) dry_run=true; shift ;;
	--force) force=true; shift ;;
	-h | --help) usage; exit 0 ;;
	*) echo "mirror-model: unknown argument $1" >&2; usage >&2; exit 2 ;;
	esac
done

die() {
	echo "mirror-model: $*" >&2
	exit 1
}

[ -n "$bucket" ] || die "--bucket is required"

for tool in aws jq; do
	command -v "$tool" >/dev/null || die "$tool is not installed"
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Build the CLI rather than expecting one on PATH: the mirror has to match the
# pin table in this checkout, not whatever version happens to be installed.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
alcatraz="$work/alcatraz"
go build -o "$alcatraz" ./cmd/alcatraz

pins_args=(models pins)
[ -n "$model" ] && pins_args+=(-model "$model")
manifest="$work/pins.json"
"$alcatraz" "${pins_args[@]}" >"$manifest"

model="$(jq -r .model "$manifest")"
revision="$(jq -r .revision "$manifest")"
source_origin="$(jq -r .origin "$manifest")"
license="$(jq -r '.license.id + " (declared at " + .license.source + ")"' "$manifest")"

# s3://bucket/prefix -> bucket, prefix
bucket_path="${bucket#s3://}"
bucket_name="${bucket_path%%/*}"
prefix="${bucket_path#"$bucket_name"}"
prefix="${prefix#/}"
prefix="${prefix%/}"

echo "$model @ ${revision:0:8}"
echo "license: $license"
echo "source:  $source_origin"
echo "dest:    s3://$bucket_name/${prefix:+$prefix/}"
echo

if [ "$dry_run" = true ]; then
	jq -r --arg p "${prefix:+$prefix/}" '.files[] | "  \($p)\(.key)  \(.size) bytes"' "$manifest"
	exit 0
fi

# Download through the normal path: every file is verified against its pinned
# digest on the way in, so nothing unverified can reach the bucket.
staging="$work/models"
"$alcatraz" models download --dest "$staging" --model "$model" >/dev/null
model_dir="$staging/$(echo "$model" | tr '/' '_')"

uploaded=0
skipped=0
while IFS=$'\t' read -r key name size; do
	s3_key="${prefix:+$prefix/}$key"
	local_file="$model_dir/$name"
	[ -f "$local_file" ] || die "$name is missing from $model_dir after download"

	if [ "$force" != true ] && aws s3api head-object --bucket "$bucket_name" --key "$s3_key" >/dev/null 2>&1; then
		echo "  skip   $s3_key (already present)"
		skipped=$((skipped + 1))
		continue
	fi

	echo "  upload $s3_key ($size bytes)"
	# A pinned revision is immutable, so it is cached forever.
	aws s3api put-object \
		--bucket "$bucket_name" \
		--key "$s3_key" \
		--body "$local_file" \
		--cache-control "public, max-age=31536000, immutable" \
		--checksum-algorithm SHA256 \
		>/dev/null
	uploaded=$((uploaded + 1))
done < <(jq -r '.files[] | [.key, .name, .size] | @tsv' "$manifest")

echo
echo "uploaded $uploaded, skipped $skipped"

# The upload is only useful if the downloader can consume it, and that is a
# different question from whether the bytes arrived: a wrong prefix, a missing
# read policy or a stray redirect all pass the upload and fail here. It is
# skippable because the bucket is often filled before anything serves it.
if [ -z "$origin" ]; then
	echo
	echo "no --origin given, skipping the round-trip verify. Once the bucket is"
	echo "served, check it with:"
	echo "  alcatraz models download --model $model --origin <base url> --dest /tmp/roundtrip"
	exit 0
fi

echo
echo "verifying round trip from $origin"
"$alcatraz" models download --dest "$work/roundtrip" --model "$model" --origin "$origin"
