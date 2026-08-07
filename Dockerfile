ARG GO_VERSION
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/pg_noty ./cmd/pg_noty
RUN apt-get update -qq \
    && apt-get install -y --no-install-recommends upx-ucl \
    && upx --best --lzma /out/pg_noty \
    && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pg_noty /pg_noty
USER nonroot:nonroot
ENTRYPOINT ["/pg_noty", "run", "-f", "/etc/pg_noty/listeners.yaml"]
