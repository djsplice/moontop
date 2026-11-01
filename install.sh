#!/bin/sh
# Moontop installer: downloads a release binary, verifies its checksum and
# installs it.
#
#   curl -fsSL https://raw.githubusercontent.com/justinh-rahb/moontop/main/install.sh | sh
#
# Environment:
#   MOONTOP_VERSION      version to install, e.g. 0.1.0 (default: latest release)
#   MOONTOP_INSTALL_DIR  where to put the binary (default: /usr/local/bin if
#                        writable, else ~/.local/bin)
set -eu

REPO="justinh-rahb/moontop"

say() { printf 'moontop: %s\n' "$*"; }
die() { printf 'moontop: error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required"; }

fetch() { # url dest
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "curl or wget is required"
	fi
}

latest_version() {
	if command -v curl >/dev/null 2>&1; then
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
	else
		url=$(wget -q --max-redirect=5 -S --spider "https://github.com/$REPO/releases/latest" 2>&1 |
			sed -n 's/^ *[Ll]ocation: *//p' | tail -1 | tr -d '\r')
	fi
	tag=${url##*/}
	case "$tag" in
	v[0-9]*) printf '%s\n' "${tag#v}" ;;
	*) die "could not find the latest release (is there one yet?)" ;;
	esac
}

detect_platform() {
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in
	linux | darwin | freebsd) ;;
	*) die "unsupported OS '$os' — on Windows use Scoop or the .zip from the releases page" ;;
	esac
	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	armv7* | armv8l) arch=armv7 ;;
	*) die "unsupported architecture '$arch'" ;;
	esac
	if [ "$os" != linux ] && [ "$arch" = armv7 ]; then
		die "no $os build for 32-bit ARM"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "sha256sum or shasum is required to verify the download"
	fi
}

main() {
	need uname
	need tar
	detect_platform
	version=${MOONTOP_VERSION:-$(latest_version)}
	version=${version#v}

	archive="moontop_${version}_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/v$version"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "downloading $archive"
	fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt"

	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$archive is not listed in checksums.txt"
	got=$(sha256 "$tmp/$archive")
	[ "$want" = "$got" ] || die "checksum mismatch for $archive"

	tar -xzf "$tmp/$archive" -C "$tmp" moontop

	dir=${MOONTOP_INSTALL_DIR:-}
	if [ -z "$dir" ]; then
		if [ -w /usr/local/bin ]; then
			dir=/usr/local/bin
		else
			dir="$HOME/.local/bin"
		fi
	fi
	mkdir -p "$dir"
	if command -v install >/dev/null 2>&1; then
		install -m 0755 "$tmp/moontop" "$dir/moontop"
	else
		cp "$tmp/moontop" "$dir/moontop" && chmod 0755 "$dir/moontop"
	fi

	say "installed $("$dir/moontop" version 2>/dev/null || echo "v$version") to $dir/moontop"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "note: $dir is not on your PATH" ;;
	esac
}

main "$@"
