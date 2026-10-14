#!/usr/bin/env bash

set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$root_dir/dist"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/mio-release.XXXXXX")"
next_dist="$(mktemp -d "$root_dir/.dist-release.XXXXXX")"

cleanup() {
    rm -rf "$work_dir"
    if [[ -n "${next_dist:-}" && -d "$next_dist" ]]; then
        rm -rf "$next_dist"
    fi
}
trap cleanup EXIT

version_output="$(cd "$root_dir" && go run ./cmd/mio version)"
version="${version_output#Mio }"
if [[ -z "$version" || "$version" == "$version_output" ]]; then
    echo "无法读取 Mio 版本" >&2
    exit 1
fi

targets=(
    "linux amd64"
    "linux arm64"
    "windows amd64"
    "darwin arm64"
)

successes=()
failures=()

for target in "${targets[@]}"; do
    read -r target_os target_arch <<<"$target"
    package="mio-${version}-${target_os}-${target_arch}"
    package_dir="$work_dir/$package"
    mkdir -p "$package_dir"

    binary_name="mio"
    if [[ "$target_os" == "windows" ]]; then
        binary_name="mio.exe"
    fi

    echo "构建 $target_os/$target_arch"
    if ! (
        cd "$root_dir"
        env CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
            go build -buildvcs=false -trimpath -ldflags="-s -w -buildid=" \
            -o "$package_dir/$binary_name" ./cmd/mio
    ); then
        failures+=("$target_os/$target_arch")
        rm -rf "$package_dir"
        continue
    fi

    cp "$root_dir/README.md" "$root_dir/CHANGELOG.md" "$package_dir/"
    cp -R "$root_dir/docs" "$root_dir/examples" "$package_dir/"

    if [[ "$target_os" == "windows" ]]; then
        (
            cd "$work_dir"
            zip -X -qr "$next_dist/$package.zip" "$package"
        )
    else
        tar -C "$work_dir" -czf "$next_dist/$package.tar.gz" "$package"
    fi
    successes+=("$target_os/$target_arch")
done

if [[ ${#successes[@]} -eq 0 ]]; then
    echo "没有平台构建成功" >&2
    exit 1
fi

(
    cd "$next_dist"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum mio-* > SHA256SUMS
    else
        shasum -a 256 mio-* > SHA256SUMS
    fi
)

rm -rf "$dist_dir"
mv "$next_dist" "$dist_dir"
next_dist=""

echo "发布文件已写入 $dist_dir"
printf '成功: %s\n' "${successes[*]}"
if [[ ${#failures[@]} -gt 0 ]]; then
    printf '失败: %s\n' "${failures[*]}" >&2
    exit 1
fi
