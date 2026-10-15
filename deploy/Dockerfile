# syntax=docker/dockerfile:1
# Build depuis la racine du dépôt : docker build -f deploy/Dockerfile -t doctolibre:dev .
# Multi-arch (amd64/arm64) : docker buildx build --platform linux/amd64,linux/arm64 ...

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/doctolibre ./cmd/doctolibre

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/doctolibre /doctolibre
USER nonroot:nonroot
EXPOSE 8080
# Pas de shell ni de curl dans l'image : le healthcheck est une sous-commande.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/doctolibre", "healthcheck"]
ENTRYPOINT ["/doctolibre"]
CMD ["serve"]
