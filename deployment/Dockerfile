FROM alpine:3.20

ARG TARGETARCH
ARG BUILD_DIR=build
ARG BINARY=csi-driver-synced-hostpath-linux-${TARGETARCH}

RUN apk add --no-cache mount

COPY ${BUILD_DIR}/${BINARY} /csi-driver-synced-hostpath

ENTRYPOINT [ "/csi-driver-synced-hostpath" ]
