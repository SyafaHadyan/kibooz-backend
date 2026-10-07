# The build runs on the machine that builds the image and cross-compiles for the target, so an arm64 image needs no emulation
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/kibooz-backend ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/kibooz-backend /kibooz-backend

EXPOSE 8080

USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/kibooz-backend", "healthcheck"]

ENTRYPOINT ["/kibooz-backend"]
