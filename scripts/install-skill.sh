#!/bin/sh

set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
source_dir="$script_dir/../skills/sticker"
destination=${1:-}

if [ -z "$destination" ]; then
	printf '%s\n' "usage: install-skill.sh DESTINATION" >&2
	exit 2
fi
if [ ! -f "$source_dir/SKILL.md" ]; then
	printf '%s\n' "sticker skill source is missing" >&2
	exit 1
fi

destination_parent=$(dirname "$destination")
mkdir -p "$destination_parent"
destination_parent=$(CDPATH= cd "$destination_parent" && pwd)
destination="$destination_parent/$(basename "$destination")"
if [ -e "$destination" ]; then
	printf 'refusing to replace existing skill: %s\n' "$destination" >&2
	exit 3
fi

if ! mkdir "$destination" 2>/dev/null; then
	printf 'refusing to replace existing skill: %s\n' "$destination" >&2
	exit 3
fi

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		rm -f "$destination/SKILL.md.tmp" 2>/dev/null || true
		rmdir "$destination" 2>/dev/null || true
	fi
	exit "$status"
}
trap cleanup EXIT HUP INT TERM

umask 022
cp "$source_dir/SKILL.md" "$destination/SKILL.md.tmp"
mv "$destination/SKILL.md.tmp" "$destination/SKILL.md"
trap - EXIT HUP INT TERM
printf 'installed sticker skill at %s\n' "$destination"
