# Console. Built first and separately, so a source edit in Go does not
# reinstall npm packages.
FROM node:22-alpine AS ui
WORKDIR /ui
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Binaries, with the console embedded.
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /ui/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/argusd ./cmd/argusd \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/argusctl ./cmd/argusctl

# Run
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/argusd /out/argusctl /usr/local/bin/
COPY configs/services.yaml /app/configs/services.yaml

ENV ARGUS_ADDR=:8080 \
    ARGUS_CATALOG_PATH=/app/configs/services.yaml \
    ARGUS_LOG_FORMAT=json
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["argusd"]
