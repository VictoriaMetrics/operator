FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/manager \
    USER_UID=1001 \
    USER_NAME=vm-operator

# install operator binary
COPY bin/manager ${OPERATOR}

ENTRYPOINT ["/usr/local/bin/manager"]

USER ${USER_UID}
