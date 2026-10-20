#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
test "$(uname -s)" = Darwin && test "$(uname -m)" = arm64 || {
  echo 'Native bootstrap verification requires macOS ARM64.' >&2
  exit 1
}
command -v codesign >/dev/null
mkdir -p build/native-bootstrap
for artifact in build/compiler-A.mochi build/compiler-B-native.mochi build/native-bootstrap/compiler-C.mochi build/native-bootstrap/compiler-D.mochi; do
  if test -e "$artifact"; then
    echo "Refusing to overwrite existing bootstrap artifact: $artifact. Run in a fresh clone." >&2
    exit 1
  fi
done
work=$(mktemp -d "$root/build/native-bootstrap-check.XXXXXX")
config=selfhost/bootstrap/bootstrap.config
cp "$config" "$work/bootstrap.config.original"
restore_config() { cp "$work/bootstrap.config.original" "$config"; }
trap restore_config EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
export MIO_BUNKO="$root/bunko"
go build -o "$work/mio" ./cmd/mio
go build -o "$work/mochi-aot" ./cmd/mochi-aot
"$work/mio" build -o build/compiler-A.mochi selfhost/bootstrap
check_hash() {
  local file=$1 expected=$2
  local actual
  actual=$(shasum -a 256 "$file" | awk '{print $1}')
  test "$actual" = "$expected" || { echo "Bootstrap hash mismatch: $file ($actual)" >&2; exit 1; }
}
check_hash build/compiler-A.mochi b5904e8fd51e96be16183826177504ed395be67143e0c78d7a4b5d304cfd00a1
compile_and_run() {
  local stage=$1 input=$2 output=$3
  local binary="$work/compiler-$stage-native"
  echo "Native bootstrap stage $stage -> $output"
  "$work/mochi-aot" -heap-mib 1024 -o "$binary" "$input"
  codesign --sign - "$binary"
  codesign --verify --strict "$binary"
  printf '%s' "$output" > "$config"
  "$binary" > "$work/compiler-$stage.stdout"
  test -s "$output"
  check_hash "$output" b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465
}
compile_and_run A build/compiler-A.mochi build/compiler-B-native.mochi
compile_and_run B build/compiler-B-native.mochi build/native-bootstrap/compiler-C.mochi
compile_and_run C build/native-bootstrap/compiler-C.mochi build/native-bootstrap/compiler-D.mochi
cmp build/compiler-B-native.mochi build/native-bootstrap/compiler-C.mochi
cmp build/native-bootstrap/compiler-C.mochi build/native-bootstrap/compiler-D.mochi
shasum -a 256 build/compiler-A.mochi build/compiler-B-native.mochi build/native-bootstrap/compiler-C.mochi build/native-bootstrap/compiler-D.mochi | tee "$work/SHA256SUMS"
# These fixtures are now generated from source, so the golden test cannot skip for missing artifacts.
go test ./internal/aot -run '^TestM17PureMioDecoderGoldenVMAndNative$' -count=1 -v -timeout 30m | tee "$work/m17-golden.log"
grep -q -- '--- PASS: TestM17PureMioDecoderGoldenVMAndNative' "$work/m17-golden.log"
if grep -q -- '--- SKIP:' "$work/m17-golden.log"; then exit 1; fi
echo "Native bootstrap fixed point verified: B = C = D. Evidence: $work"
