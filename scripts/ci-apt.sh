#!/bin/sh
# ci-apt.sh PKG...
# Installs Debian packages on a CI runner without hanging on a slow
# mirror. A stalled connection gives up after 30 s and apt retries it;
# each apt-get run is cut off after a limit and tried again, up to three
# times (at worst about 19 minutes in all, inside the jobs' 25).
# Downloaded .debs go to $APT_ARCHIVES (default ~/.cache/apt-archives) so
# the workflow can cache them (.github/actions/gtk): with the cache
# restored, install downloads only what changed since.
set -u
archives=${APT_ARCHIVES:-$HOME/.cache/apt-archives}
mkdir -p "$archives/partial"
opts="-o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30 -o Dir::Cache::Archives=$archives"

# attempt LIMIT CMD... runs CMD up to three times, each cut off after LIMIT.
attempt() {
	limit=$1
	shift
	for n in 1 2 3; do
		if timeout "$limit" "$@"; then
			return 0
		fi
		echo "::warning::$* failed or took over $limit (try $n of 3)"
		# A run cut off while unpacking leaves dpkg half done.
		sudo dpkg --configure -a >/dev/null 2>&1 || true
		sleep $((n * 5))
	done
	return 1
}

# shellcheck disable=SC2086 # $opts is a word list on purpose
attempt 2m sudo apt-get $opts update || exit 1
# shellcheck disable=SC2086
attempt 4m sudo apt-get $opts install -y --no-install-recommends "$@" || exit 1
# Drop .debs no longer installable, so the cache does not grow.
# shellcheck disable=SC2086
sudo apt-get $opts autoclean -y >/dev/null || true
# The cache step runs as the runner user; apt wrote these as root.
sudo chown -R "$(id -u):$(id -g)" "$archives"
rm -rf "$archives/partial" "$archives/lock"
