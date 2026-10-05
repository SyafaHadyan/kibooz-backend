FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/kibooz-backend ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/kibooz-backend /kibooz-backend

EXPOSE 8080

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/kibooz-backend", "healthcheck"]

ENTRYPOINT ["/kibooz-backend"]
