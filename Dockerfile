ARG BASE_IMAGE=alpine:3.20
FROM ${BASE_IMAGE}
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

ARG TARGETARCH

# add binary
ADD build/linux/${TARGETARCH}/pdb-controller /

ENTRYPOINT ["/pdb-controller"]
