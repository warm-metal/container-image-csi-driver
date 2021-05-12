FROM alpine:3
ENV TARGET1=""
ENV TARGET2=""
WORKDIR /
COPY stat-fs.sh /
ENTRYPOINT ["/stat-fs.sh", "$TARGET1", "$TARGET2"]
