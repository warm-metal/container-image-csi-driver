FROM alpine:3.23.3
# Ensure we have the latest packages including libssl and remove cache
RUN apk update && \
    apk upgrade && \
    apk add --no-cache libssl3 libcrypto3 && \
    rm -rf /var/cache/apk/*
ENV TARGET1=""
ENV TARGET2=""
WORKDIR /
COPY stat-fs.sh /
RUN chmod +x /stat-fs.sh
ENTRYPOINT ["/stat-fs.sh", "$TARGET1", "$TARGET2"]
