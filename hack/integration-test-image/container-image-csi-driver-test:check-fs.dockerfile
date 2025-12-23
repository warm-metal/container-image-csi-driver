FROM alpine:3.23.0
ENV TARGET=""
WORKDIR /
# Ensure we have the latest packages and remove cache
RUN apk update && apk upgrade && rm -rf /var/cache/apk/*
COPY check-fs.sh /
RUN chmod +x /check-fs.sh
ENTRYPOINT ["/check-fs.sh", "$TARGET"]
