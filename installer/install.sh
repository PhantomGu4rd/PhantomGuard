#!/usr/bin/env sh
# Install a downloaded PhantomGuard Unix release into a user-writable bin dir.
set -eu

usage() {
  cat <<'EOF'
Usage: ./install.sh [--source <binary>] [--install-dir <directory>]

Installs the downloaded PhantomGuard binary without requiring sudo or Go.
Defaults:
  source:      ./phantomguard (next to this script)
  install dir: $PHANTOMGUARD_INSTALL_DIR, or ~/.local/bin
EOF
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_path="$script_dir/phantomguard"
install_dir="${PHANTOMGUARD_INSTALL_DIR:-$HOME/.local/bin}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source)
      [ "$#" -ge 2 ] || { echo "install.sh: --source requires a value" >&2; exit 2; }
      source_path=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || { echo "install.sh: --install-dir requires a value" >&2; exit 2; }
      install_dir=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown option $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ ! -f "$source_path" ]; then
  echo "install.sh: binary not found: $source_path" >&2
  exit 2
fi

mkdir -p "$install_dir"
destination="$install_dir/phantomguard"
cp "$source_path" "$destination"
chmod 755 "$destination"

install_linux_desktop_assets() {
  package_share="$script_dir/share"
  icon_source="$package_share/icons/hicolor/768x768/apps/phantomguard.png"
  launcher_source="$package_share/applications/phantomguard.desktop"
  [ -f "$icon_source" ] && [ -f "$launcher_source" ] || return 0

  data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
  icon_directory="$data_home/icons/hicolor/768x768/apps"
  launcher_directory="$data_home/applications"
  mkdir -p "$icon_directory" "$launcher_directory"
  cp "$icon_source" "$icon_directory/phantomguard.png"
  cp "$launcher_source" "$launcher_directory/phantomguard.desktop"

  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$launcher_directory" >/dev/null 2>&1 || true
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f "$data_home/icons/hicolor" >/dev/null 2>&1 || true
  fi
  echo "Installed the PhantomGuard desktop launcher and icon."
}

install_linux_desktop_assets

echo "Installed PhantomGuard at $destination"
case ":${PATH}:" in
  *":${install_dir}:"*)
    echo "Run: phantomguard --help"
    ;;
  *)
    echo "Add this directory to PATH, then open a new terminal: $install_dir"
    ;;
esac
