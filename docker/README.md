# Mio Full offline development candidate (linux/amd64)

The planned stable name is `ghcr.io/mio-kobo/mio:2.1.1-full`. This PR does **not**
push that image or any other registry image. Download the Docker candidate tar
and SHA256SUMS from the PR's `mio-docker-linux-amd64-candidate-*` Actions artifact:

```sh
sha256sum -c SHA256SUMS
docker load -i mio-linux-amd64-full-candidate.docker.tar
docker run --rm -it --platform linux/amd64 --network none \
  --user "$(id -u):$(id -g)" --env HOME=/tmp \
  --mount "type=bind,src=$PWD,dst=/workspace" \
  mio:2.1.1-full-pr-candidate
```

The image defaults to UID/GID 1000:1000, `/workspace`, and `/bin/sh`. On Linux,
`--user` matches the host's ownership for bind-mounted projects; use a directory
that your host user can write. The image does not chown host mounts. Omit the
mount to use the image's writable workspace as UID 1000. Mounts preserve project
files and generated native images after container exit. Docker Desktop on macOS
and Windows maps Linux-container mounts through its VM; PowerShell users should
pass their absolute host directory and a suitable numeric `--user` when needed.

Inside the offline shell:

```sh
mio version
printf 'chant("offline hello");\n' > hello.mio
mio build hello.mio --target=linux-amd64 -o hello
./hello
mio run hello.mio --target=linux-amd64
printf 'yobu "kiso/math";\nchant(math.abs(-9));\n' > math.mio
mio check math.mio
mio build math.mio --target=linux-amd64 -o math
./math
mio build hello.mio --emit=mochi -o hello.mochi
mio disasm hello.mochi
mio test your-tests
mio lsp --stdio
```

The image provides `/opt/mio/bin/mio`, license, manifest and provenance. Full
embeds the complete Bunko standard library, so the image has no external
`bunko/` directory and does not set `MIO_BUNKO`. PATH includes `/opt/mio/bin`.
The historical CLI build default is Darwin; specify
`--target=linux-amd64` for native Linux build/run/exec commands. Test selects the
Linux host automatically. Full's integrated tools are preserved; no unverified
standalone tools are added.

Base: official Alpine **3.22.2**, pinned to its linux/amd64 manifest
`sha256:85f2b723e106c34644cd5851d7e81ee87da98ac54672b29947c052a45d31dc2f`.
It supplies the interactive POSIX shell and BusyBox development utilities (cp,
vi, grep, sed, awk, tar, sha256sum). The Mio ELF is checked using ELF program
headers and `readelf -h -l -d`; PT_INTERP/PT_DYNAMIC fail the build. The current
syscall-only backend needs no glibc/musl loader, C compiler or external linker.
The verification record contains the actual linkage evidence. No startup script
clones repositories, downloads Bunko or installs anything. Go and Python are
absent at runtime. Package-manager/network operations and any dependencies
outside the Full package are outside the offline promise.

To reproduce on a Linux AMD64 build host with Docker, Python 3 and binutils:

```sh
python docker/build.py --candidates /path/to/verified-linux-candidate-artifact \
  --output /tmp/mio-docker-candidate
```

This script consumes the verified Full tar.gz and its SHA256SUMS plus successful
`verification.json`; checks the manifest/provenance and embedded-Bunko provenance; copies those exact
bytes; compares the container compiler hash with Full; runs `--network=none`
build, run, embedded Bunko import, check, disasm, test and LSP tests as the host's non-root UID/GID in
an external directory containing spaces/Unicode; checks host persistence; then
saves, removes, loads and tests the image again. Any failed command fails CI.
Docker may pull the pinned base while building; all container tests and Dockerfile
RUN instructions have networking disabled. No registry credentials are used.

Only linux/amd64 is covered. Apple Silicon may try `--platform linux/amd64`
emulation; it is not native ARM64 support or a performance claim. Windows needs
Linux containers. No linux/arm64 image is generated.

Docker binding/UID behavior follows the [Docker run documentation](https://docs.docker.com/engine/containers/run/).
See [Alpine's official image](https://hub.docker.com/_/alpine) for base-image details.
