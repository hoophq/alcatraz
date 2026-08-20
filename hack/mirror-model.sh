#!/usr/bin/env bash
#
# Mirror a pinned model into an S3 bucket laid out like the hub, then prove the
# result by downloading it back through the origin an operator would use.
#
# Two trees are published. The hub-shaped one is content addressed and
# write-once, and is what "models download --origin" asks for. Beside it,
# current/ is a moving alias laid out the way "models download --dest" writes
# on disk, with a checksums.txt covering it: that is for consumers who have
# curl and nothing else, and it keeps the revision and the digests out of their
# build files.
#
# The file list, digests and keys all come from "alcatraz models pins", so this
# script never carries a second copy of the pin table.
#
#   hack/mirror-model.sh --bucket s3://bucket/prefix \
#                        --origin https://host/prefix
#
# Deliberately generic: this script knows nothing about Hoop's buckets, and
# both addresses arrive on the command line. The pair the workflows publish to
# is defined once, in hack/mirror-target.env — the only place either URL is
# written down, so changing where the mirror lives is a one-line edit.
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
because a pinned revision that changes bytes is a mistake, not an update. The
current/ alias is the exception, and is rewritten on every run.
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

for tool in aws jq curl; do
	command -v "$tool" >/dev/null || die "$tool is not installed"
done

# GNU coreutils on a CI runner, the perl shasum on a mac laptop.
sha256_check() {
	if command -v sha256sum >/dev/null; then
		sha256sum -c "$1"
	else
		shasum -a 256 -c "$1"
	fi
}

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

# The alias reproduces the on-disk layout, so its directory name is the model
# id with "/" replaced by "_", matching models.Dir and hugot.
model_dirname="$(echo "$model" | tr '/' '_')"
alias_prefix="${prefix:+$prefix/}current"
model_alias="$alias_prefix/$model_dirname"

echo "$model @ ${revision:0:8}"
echo "license: $license"
echo "source:  $source_origin"
echo "dest:    s3://$bucket_name/${prefix:+$prefix/}"
echo "alias:   s3://$bucket_name/$alias_prefix/"
# Printed beside the two write addresses, not only where it is used at the
# end: the read address is the one nothing else in this script can derive, and
# a pair that does not correspond is only obvious when you can see both.
echo "verify:  ${origin:-(skipped, no --origin given)}"
echo

if [ "$dry_run" = true ]; then
	jq -r --arg p "${prefix:+$prefix/}" '.files[] | "  \($p)\(.key)  \(.size) bytes"' "$manifest"
	jq -r --arg d "$model_alias/" '.files[] | "  \($d)\(.name)  \(.size) bytes"' "$manifest"
	echo "  $alias_prefix/checksums.txt"
	exit 0
fi

# Download through the normal path: every file is verified against its pinned
# digest on the way in, so nothing unverified can reach the bucket.
staging="$work/models"
"$alcatraz" models download --dest "$staging" --model "$model" >/dev/null
model_dir="$staging/$model_dirname"

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

# The alias is a copy, not a redirect: S3 has no symlinks. Copy it server side
# so a republish does not push the model through the runner twice. These keys
# are overwritten every run, --force or not, which is what makes it an alias.
#
# It must not be cached immutably like the pinned tree, or it would stop
# moving. A short ttl means a build that runs in the minutes after a pin change
# can see an old manifest against new files, which sha256sum -c then fails on:
# loud and retryable, rather than a wrong model that boots.
alias_cache="public, max-age=300"

echo
echo "refreshing $alias_prefix/"
while IFS=$'\t' read -r key name; do
	aws s3api copy-object \
		--bucket "$bucket_name" \
		--key "$model_alias/$name" \
		--copy-source "$bucket_name/${prefix:+$prefix/}$key" \
		--metadata-directive REPLACE \
		--cache-control "$alias_cache" \
		--checksum-algorithm SHA256 \
		>/dev/null
	echo "  copy   $model_alias/$name"
done < <(jq -r '.files[] | [.key, .name] | @tsv' "$manifest")

# checksums.txt covers the whole alias directory, so it is rebuilt from every
# pinned model already mirrored there, not just this run's. Regenerating it
# from one model would erase the others the first time a second one is pinned.
checksums="$work/checksums.txt"
: >"$checksums"
while read -r pinned; do
	pinned_dirname="$(echo "$pinned" | tr '/' '_')"
	pinned_pins="$work/pins-$pinned_dirname.json"
	"$alcatraz" models pins -model "$pinned" >"$pinned_pins"

	# Every file, at its pinned size, not a probe of the first one: a run
	# that died mid-copy leaves an object in place, and listing the rest
	# would advertise keys that are not there. Consumers read this as
	# authoritative and fetch every line.
	complete=true
	while IFS=$'\t' read -r name size; do
		got="$(aws s3api head-object --bucket "$bucket_name" \
			--key "$alias_prefix/$pinned_dirname/$name" \
			--query ContentLength --output text 2>/dev/null)" || got=""
		[ "$got" = "$size" ] || { complete=false; break; }
	done < <(jq -r '.files[] | [.name, .size] | @tsv' "$pinned_pins")

	if [ "$complete" != true ]; then
		echo "  omit   $pinned_dirname (not complete under $alias_prefix/)"
		continue
	fi

	jq -r --arg d "$pinned_dirname" '.files[] | "\(.sha256)  \($d)/\(.name)"' "$pinned_pins" >>"$checksums"
done < <("$alcatraz" models pins -list)

aws s3api put-object \
	--bucket "$bucket_name" \
	--key "$alias_prefix/checksums.txt" \
	--body "$checksums" \
	--content-type "text/plain; charset=utf-8" \
	--cache-control "$alias_cache" \
	--checksum-algorithm SHA256 \
	>/dev/null
echo "  put    $alias_prefix/checksums.txt ($(grep -c . "$checksums") files)"

# The upload is only useful if the downloader can consume it, and that is a
# different question from whether the bytes arrived: a wrong prefix, a missing
# read policy or a stray redirect all pass the upload and fail here. It is
# skippable because the bucket is often filled before anything serves it.
if [ -z "$origin" ]; then
	echo
	echo "no --origin given, skipping the round-trip verify. Once the bucket is"
	echo "served, check it with:"
	echo "  alcatraz models download --model $model --origin <base url> --dest /tmp/roundtrip"
	echo "  curl -fsSL <base url>/current/checksums.txt"
	exit 0
fi

origin="${origin%/}"

echo
echo "verifying round trip from $origin"
"$alcatraz" models download --dest "$work/roundtrip" --model "$model" --origin "$origin"

# The alias answers to a different consumer — curl and sha256sum, no alcatraz —
# so it is checked the way that consumer reads it: take the manifest, fetch
# what it names, verify against it. Nothing here knows the revision.
echo
echo "verifying $origin/current"
alias_dir="$work/alias"
mkdir -p "$alias_dir"
curl -fsSL "$origin/current/checksums.txt" -o "$alias_dir/checksums.txt"

# Everything else here proves the bytes are servable. This proves they are the
# bytes *this run* wrote. An origin fronting some other bucket answers every
# request above perfectly well, out of a mirror somebody else filled: the model
# is the same pinned revision, so the digests match and the round trip is
# green while the upload went somewhere no consumer reads.
#
# The manifest is the one artifact rewritten unconditionally on every run, and
# it is built from what a head-object found in *this* bucket — so a bucket that
# did not receive the upload yields a manifest that disagrees, an empty one in
# the case that matters, a fresh prefix nobody has published to yet.
#
# It cannot catch an origin serving a byte-identical mirror of the same pins.
# Nothing cheap can: that failure is indistinguishable from success by content
# alone. The workflow closes it from the other side, by refusing an origin that
# is not paired with the bucket (hack/mirror-target.env).
if ! diff -u --label uploaded --label served "$checksums" "$alias_dir/checksums.txt"; then
	die "$origin/current/checksums.txt is not the manifest this run uploaded to s3://$bucket_name/$alias_prefix/: --origin does not serve --bucket"
fi

while read -r _ file; do
	mkdir -p "$alias_dir/$(dirname "$file")"
	curl -fsSL "$origin/current/$file" -o "$alias_dir/$file"
done <"$alias_dir/checksums.txt"
(cd "$alias_dir" && sha256_check checksums.txt)
