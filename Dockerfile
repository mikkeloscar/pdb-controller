FROM golang:1.12.4-alpine3.9 as builder

WORKDIR /pdb-controller

RUN apk --no-cache add git

ADD .  /pdb-controller
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-extldflags '-static'" -o pdb-controller

FROM alpine:3.9 as runner

COPY --from=builder /pdb-controller/pdb-controller /usr/bin/pdb-controller

ENTRYPOINT ["pdb-controller"]
