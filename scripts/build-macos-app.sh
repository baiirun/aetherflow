#!/bin/sh
set -eu

usage() {
    cat <<'EOF'
Usage: scripts/build-macos-app.sh [--install | --install-to DIRECTORY]

Builds target/release/bundle/Aetherflow.app and ad-hoc signs it.
  --install               Install to /Applications.
  --install-to DIRECTORY  Install to another Applications directory.
EOF
}

install_directory=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --install)
            install_directory="/Applications"
            shift
            ;;
        --install-to)
            if [ "$#" -lt 2 ]; then
                echo "--install-to requires a directory" >&2
                exit 2
            fi
            install_directory="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if [ "$(uname -s)" != "Darwin" ]; then
    echo "Aetherflow.app can only be built on macOS" >&2
    exit 1
fi
if [ "$(uname -m)" != "arm64" ]; then
    echo "the bundled Rivet Engine currently supports Apple Silicon only" >&2
    exit 1
fi

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_directory/.." && pwd)
bundle_root="$repository_root/target/release/bundle"
bundle="$bundle_root/Aetherflow.app"
contents="$bundle/Contents"
macos_directory="$contents/MacOS"
helpers_directory="$contents/Helpers"
resources_directory="$contents/Resources"
licenses_directory="$resources_directory/Licenses"

package_version=$(
    sed -n \
        '/^\[workspace.package\]/,/^\[workspace.dependencies\]/s/^version = "\([^"]*\)"/\1/p' \
        "$repository_root/Cargo.toml"
)
if [ -z "$package_version" ]; then
    echo "could not read the workspace package version" >&2
    exit 1
fi

cd "$repository_root"
cargo build --release --bin aetherflow-desktop --bin aetherflowd

case "$bundle" in
    "$repository_root"/target/release/bundle/Aetherflow.app) ;;
    *)
        echo "refusing to replace unexpected bundle path: $bundle" >&2
        exit 1
        ;;
esac
rm -rf "$bundle"
mkdir -p "$macos_directory" "$helpers_directory" "$licenses_directory"

install -m 755 "$repository_root/target/release/aetherflow-desktop" "$macos_directory/Aetherflow"
install -m 755 "$repository_root/target/release/aetherflowd" "$helpers_directory/aetherflowd"
install -m 644 "$repository_root/assets/macos/AppIcon.icns" "$resources_directory/AppIcon.icns"
install -m 644 "$repository_root/LICENSE" "$licenses_directory/Aetherflow.txt"
install -m 644 "$repository_root/vendor/rivet-engine/LICENSE" "$licenses_directory/RivetEngine.txt"
sed "s/@VERSION@/$package_version/g" \
    "$repository_root/assets/macos/Info.plist" > "$contents/Info.plist"

plutil -lint "$contents/Info.plist" >/dev/null
codesign --force --sign - "$helpers_directory/aetherflowd"
codesign --force --sign - "$bundle"
codesign --verify --deep --strict --verbose=2 "$bundle"

echo "Built $bundle"

if [ -n "$install_directory" ]; then
    mkdir -p "$install_directory"
    installed_bundle="$install_directory/Aetherflow.app"
    case "$installed_bundle" in
        */Aetherflow.app) ;;
        *)
            echo "refusing to replace unexpected install path: $installed_bundle" >&2
            exit 1
            ;;
    esac
    rm -rf "$installed_bundle"
    ditto "$bundle" "$installed_bundle"
    codesign --verify --deep --strict --verbose=2 "$installed_bundle"
    echo "Installed $installed_bundle"
fi
