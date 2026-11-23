# The build runs on the machine that builds the image and cross-compiles for the target, so an arm64 image needs no emulation
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# the exporter of the bucket sizes is a second small program in the same image, which only the service bucket-sizes of the production
# example starts, with its own entrypoint
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/kibooz-backend ./cmd/api \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/bucketsizes ./cmd/bucketsizes

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5

COPY --from=build /out/kibooz-backend /kibooz-backend
COPY --from=build /out/bucketsizes /bucketsizes

EXPOSE 8080

USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/kibooz-backend", "healthcheck"]

ENTRYPOINT ["/kibooz-backend"]
