# Multi-stage build: compile a static binary, then package it in a minimal
# distroless image. The result is ~15MB and has no shell or package manager,
# which is ideal for a CLI that runs against a kubeconfig.
#
# The binary needs a writable HOME and a kubeconfig to work out-of-the-box;
# see README for the recommended `docker run -v ~/.kube:/root/.kube` usage.
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X k8s-ctx-dumper/cmd.version=${VERSION} \
      -X k8s-ctx-dumper/cmd.commit=${COMMIT} \
      -X k8s-ctx-dumper/cmd.date=${BUILD_DATE}" \
    -o /out/k8s-ctx-dumper .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/k8s-ctx-dumper /usr/local/bin/k8s-ctx-dumper
ENTRYPOINT ["/usr/local/bin/k8s-ctx-dumper"]
