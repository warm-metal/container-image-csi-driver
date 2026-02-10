FROM alpine:3.23.3
ENV TARGET=""
WORKDIR /
# Ensure we have the latest packages including libssl and remove cache
RUN apk update && \
    apk upgrade && \
    apk add --no-cache libssl3 libcrypto3 && \
    rm -rf /var/cache/apk/*
COPY check-fs.sh /
RUN chmod +x /check-fs.sh
ENTRYPOINT ["/check-fs.sh", "$TARGET"]
