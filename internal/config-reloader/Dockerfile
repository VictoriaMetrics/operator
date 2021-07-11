ARG base_image

FROM ${base_image}
# src_binary arg must be in scope, after FROM

ARG ARCH

ENV USER_UID=1001 \
    USER_NAME=config-reloader

# install operator binary
COPY bin/config-reloader-${ARCH} /usr/local/bin/config-reloader

ENTRYPOINT ["/usr/local/bin/config-reloader"]

USER ${USER_UID}
