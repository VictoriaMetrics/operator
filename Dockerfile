ARG ROOT_IMAGE
ARG USER_UID=1001
ARG APP_NAME=operator

FROM alpine AS passwd

ARG TARGETARCH
ARG USER_UID
ARG APP_NAME

ENV USER_UID=${USER_UID} \
    USER_NAME=${APP_NAME}
RUN adduser -S -D -u ${USER_UID} -s /bin/false ${APP_NAME} && \
    cat /etc/passwd | grep ${APP_NAME} > /etc/passwd_export

FROM ${ROOT_IMAGE}
# src_binary arg must be in scope, after FROM

ARG TARGETARCH
ARG USER_UID
ARG APP_NAME
ARG APP_PATH=/usr/local/bin/${APP_NAME}

ENV USER_UID=${USER_UID} \
    USER_NAME=${APP_NAME}

COPY bin/${APP_NAME}-${TARGETARCH} ${APP_NAME}

COPY --from=passwd /etc/passwd_export /etc/passwd

ENTRYPOINT ["${APP_PATH}"]

USER ${USER_NAME}
