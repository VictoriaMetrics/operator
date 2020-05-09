FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/vm-operator \
    USER_UID=1001 \
    USER_NAME=vm-operator

# install operator binary
COPY vm-operator ${OPERATOR}

ENTRYPOINT ["/usr/local/bin/vm-operator"]

USER ${USER_UID}
