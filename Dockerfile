FROM registry.opensource.zalan.do/library/alpine-3.13:latest
MAINTAINER Team Teapot @ Zalando SE <team-teapot@zalando.de>

# add binary
ADD build/linux/pdb-controller /

ENTRYPOINT ["/pdb-controller"]
