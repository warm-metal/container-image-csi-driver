FROM alpine:3.23.0
# Ensure we have the latest packages and remove cache
RUN apk update && apk upgrade && rm -rf /var/cache/apk/*
ENV TARGET1=""
ENV TARGET2=""
WORKDIR /
COPY stat-fs.sh /
RUN chmod +x /stat-fs.sh
ENTRYPOINT ["/stat-fs.sh", "$TARGET1", "$TARGET2"]
