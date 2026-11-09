# linux/amd64 manifest of the official Alpine 3.22.2 image, pinned at inspection.
FROM alpine:3.22.2@sha256:85f2b723e106c34644cd5851d7e81ee87da98ac54672b29947c052a45d31dc2f
ARG MIO_COMPILER_SHA256
ARG MIO_SOURCE_SHA
LABEL org.opencontainers.image.title="Mio Full offline development candidate" \
      org.opencontainers.image.source="https://github.com/mio-kobo/mio" \
      org.opencontainers.image.revision="$MIO_SOURCE_SHA" \
      io.mio.compiler.sha256="$MIO_COMPILER_SHA256"
COPY full/ /opt/mio/
RUN test -n "$MIO_COMPILER_SHA256" \
    && echo "$MIO_COMPILER_SHA256  /opt/mio/bin/mio" | sha256sum -c - \
    && addgroup -g 1000 mio && adduser -D -u 1000 -G mio mio \
    && mkdir /workspace && chown mio:mio /workspace
ENV PATH="/opt/mio/bin:$PATH" HOME="/home/mio"
WORKDIR /workspace
USER 1000:1000
CMD ["/bin/sh"]
