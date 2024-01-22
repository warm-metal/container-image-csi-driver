FROM alpine:3
ENV TARGET=""
WORKDIR /
COPY check-fs.sh /
ENTRYPOINT ["/check-fs.sh", "$TARGET"]
