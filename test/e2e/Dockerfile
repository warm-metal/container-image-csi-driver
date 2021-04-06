FROM ubuntu:16.04

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

WORKDIR /root
RUN curl --location https://storage.googleapis.com/kubernetes-release/release/v1.20.5/kubernetes-test-linux-amd64.tar.gz | \
      tar --strip-components=3 -zxf - kubernetes/test/bin/e2e.test kubernetes/test/bin/ginkgo

COPY driver.yaml .
ENTRYPOINT ./e2e.test -ginkgo.v -ginkgo.focus='External.Storage' -storage.testdriver=driver.yaml