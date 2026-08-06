#!/usr/bin/env bash
# agenton installer — puts the agenton binary on your PATH. Phone access rides
# the system Tailscale app; agenton embeds no Tailscale node. Re-runnable.
#
#   ./install.sh                 # asks where to install (default /usr/local/bin)
#   ./install.sh -d ~/bin        # or name the directory outright
#   AGENTON_INSTALL_DIR=~/bin ./install.sh
#   curl -fsSL https://raw.githubusercontent.com/nduwork/agenton-pocket/main/install.sh | bash
set -euo pipefail

REPO_SLUG="nduwork/agenton-pocket"
DEFAULT_DIR=/usr/local/bin

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported arch: $arch" ;;
esac

# --- where to install --------------------------------------------------------
# An explicitly chosen directory is honored exactly: if it turns out to be
# unwritable we fail instead of quietly installing somewhere else. Only the
# unattended default may fall back to ~/.local/bin.
install_dir="${AGENTON_INSTALL_DIR:-}"
dir_explicit=0
[ -n "$install_dir" ] && dir_explicit=1

while [ $# -gt 0 ]; do
	case "$1" in
		-d | --dir) install_dir="${2:?-d needs a directory}"; dir_explicit=1; shift ;;
		-h | --help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) die "unknown argument: $1 (try -h)" ;;
	esac
	shift
done

# expand a leading ~ ourselves: it is literal inside a variable
expand_tilde() {
	# shellcheck disable=SC2088  # these are patterns matching a literal '~',
	# not a tilde we expect the shell to expand
	case "$1" in
		"~") printf '%s' "$HOME" ;;
		"~/"*) printf '%s' "$HOME/${1#\~/}" ;;
		*) printf '%s' "$1" ;;
	esac
}

if [ -z "$install_dir" ]; then
	# Ask, but never read from stdin: when piped (`curl … | bash`) stdin IS the
	# script, so a plain `read` would eat the rest of it. Prompt only when a
	# terminal is actually attached, and take it from /dev/tty.
	answer=""
	if [ -t 0 ]; then
		read -r -p "Install agenton to [$DEFAULT_DIR]: " answer || answer=""
	elif [ -t 2 ] && [ -r /dev/tty ]; then
		printf 'Install agenton to [%s]: ' "$DEFAULT_DIR" >&2
		read -r answer < /dev/tty || answer=""
	fi
	if [ -n "$answer" ]; then
		install_dir="$answer"
		dir_explicit=1        # typed by hand → honor it strictly
	else
		install_dir="$DEFAULT_DIR"
	fi
fi
install_dir=$(expand_tilde "$install_dir")

# install_bin SRC — install a binary into $install_dir, using sudo if needed.
# Creating the dir counts as writability: ~/bin may not exist yet.
install_bin() {
	local src="$1" dest="$install_dir"
	if mkdir -p "$dest" 2>/dev/null && [ -w "$dest" ]; then
		install -m 0755 "$src" "$dest/agenton"
	elif sudo -v 2>/dev/null; then
		say "Installing to $dest (sudo)…"
		sudo install -d -m 0755 "$dest"
		sudo install -m 0755 "$src" "$dest/agenton"
	elif [ "$dir_explicit" -eq 1 ]; then
		die "cannot write to $dest and sudo is unavailable.
       Pick a directory you own, e.g.: ./install.sh -d \"\$HOME/.local/bin\""
	else
		# Unattended default only: nowhere to write, so use the user's own bin.
		dest="$HOME/.local/bin"
		warn "$install_dir is not writable and sudo is unavailable — using $dest"
		mkdir -p "$dest"
		install -m 0755 "$src" "$dest/agenton"
	fi
	say "installed: $dest/agenton"
	case ":$PATH:" in
		*":$dest:"*) ;;
		*) warn "$dest is not on your PATH — add it:"
		   echo "       echo 'export PATH=\"$dest:\$PATH\"' >> ~/.zshrc" ;;
	esac
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# 1. Prefer a prebuilt release binary ----------------------------------------
# Resolve the latest tag via the /releases/latest redirect (the final URL ends
# in /tag/<version>). This avoids api.github.com, whose unauthenticated 60/hr
# rate limit trips on shared NAT / CI. Then fetch the archive matching
# goreleaser's name_template: agenton_<version>_<os>_<arch>.tar.gz (no leading v).
latest_url=$(curl -fsSLo /dev/null -w '%{url_effective}' \
	"https://github.com/$REPO_SLUG/releases/latest" 2>/dev/null || true)
tag="${latest_url##*/tag/}"
[ "$tag" = "$latest_url" ] && tag=""   # no /tag/ in the URL → couldn't resolve

if [ -n "$tag" ]; then
	ver="${tag#v}"
	url="https://github.com/$REPO_SLUG/releases/download/$tag/agenton_${ver}_${os}_${arch}.tar.gz"
	if curl -fsSL "$url" -o "$tmp/a.tgz" 2>/dev/null; then
		say "Downloaded agenton $tag ($os/$arch)."
		tar -xzf "$tmp/a.tgz" -C "$tmp"
		install_bin "$tmp/agenton"
	else
		tag=""  # asset missing for this platform; fall through to source build
	fi
fi

# 2. Fall back to building from source ----------------------------------------
if [ -z "$tag" ]; then
	if ! command -v go >/dev/null; then
		die "couldn't fetch a release binary (no network, or none published yet) and Go isn't installed.
       Install Go (https://go.dev/dl/) and re-run, or download a build from
       https://github.com/$REPO_SLUG/releases and put 'agenton' on your PATH."
	fi
	[ -f go.mod ] || die "no release binary fetched and go.mod not found — run this from a clone of the repo, or install a release from https://github.com/$REPO_SLUG/releases"
	say "Building agenton from source…"
	go build -o "$tmp/agenton" ./cmd/agenton
	install_bin "$tmp/agenton"
fi

# Done ------------------------------------------------------------------------
cat <<'EOF'

Setup complete. From anywhere:
       agenton vpn           # start over your tailnet (Tailscale): daemon + web + TUI
       agenton lan           # start over your local network (same Wi-Fi) instead
       agenton vpn -no-tui   # headless server
       agenton               # resume the running session

With the Tailscale app running, agenton publishes the phone bridge over your
tailnet and prints a connect QR — nothing to approve, no login link. Reprint it
any time with `agenton qr`. (On the phone you also need the Tailscale app,
signed into the same account, to be on the tailnet.)
EOF
