#!/bin/sh
set -eu
[ "$(id -u)" != 0 ]
[ "$PWD" = /workspace ]
! command -v go
! command -v python
! command -v python3
mio version
printf 'chant("offline hello");\n' > hello.mio
mio build hello.mio --target=linux-amd64 -o hello
[ "$(./hello)" = 'offline hello' ]
[ "$(mio run hello.mio --target=linux-amd64)" = 'offline hello' ]
printf 'yobu "kiso/math";\nchant(math.abs(-9));\n' > math.mio
mio check math.mio
mio build math.mio --target=linux-amd64 -o math
[ "$(./math)" = 9 ]
# Verify the embedded table with the environment override removed.
env -u MIO_BUNKO mio check math.mio
mio build hello.mio --emit=mochi -o hello.mochi
mio disasm hello.mochi > hello.disasm
[ "$(mio exec hello.mochi --target=linux-amd64)" = 'offline hello' ]
[ -s hello.disasm ]
mkdir -p sample
cat > sample/sample_test.mio <<'MIO'
yobu "kiso/testing";
yobu "kiso/math";
waza test_offline_math() -> unit { testing.expect(math.abs(-9)==9,"offline math"); }
MIO
mio test sample > tests.txt
cat tests.txt
grep '1 passed' tests.txt
# A finite stdio session exercises the integrated native LSP without a network.
# Frame lengths are generated on the host by build.py to avoid hand-count errors.
mio lsp --stdio < lsp-input > lsp-output
grep 'capabilities' lsp-output
printf 'persisted\n' > persisted.txt
