FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3

ENV DIR=/usr/local/bin/retag-image \
    USER_UID=1001 \
    USER_NAME=retag-image

# install operator binary
COPY build/_output/bin/retag-imagelinuxamd64 ${DIR}

COPY build/bin /usr/local/bin

ENTRYPOINT ["/usr/local/bin/retag-image"]

USER ${USER_UID}

