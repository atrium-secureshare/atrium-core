# syntax=docker/dockerfile:1

# Frontend stage: build the recipient SPA. Vite emits into internal/webui/dist
# (see web/vite.config.ts), which the Go build then embeds into the binary.
FROM node:24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
# Collect license notices for the frontend's runtime dependencies (fails the
# build if any shipped dependency has no detectable license).
RUN node scripts/collect-licenses.mjs > third-party-npm.txt

# Build stage: compile a static binary with the embedded frontend.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Pinned for reproducible third-party notices; also reproduces Apache-2.0 NOTICE files.
RUN go install github.com/google/go-licenses@v1.6.0
COPY . .
# Overlay the built SPA so //go:embed all:dist picks up the real bundle instead
# of the committed placeholder.
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
COPY --from=web /src/web/third-party-npm.txt ./third-party-npm.txt
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/atrium ./cmd/atrium
# Assemble THIRD-PARTY-LICENSES (Go module licenses + frontend npm notices).
RUN sh scripts/build-notices.sh ./third-party-npm.txt /out/THIRD-PARTY-LICENSES

# Runtime stage: distroless, non-root.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/atrium /atrium
# Ship the project license and the aggregated third-party notices alongside the binary.
COPY LICENSE /LICENSE
COPY --from=build /out/THIRD-PARTY-LICENSES /THIRD-PARTY-LICENSES
EXPOSE 8080
ENTRYPOINT ["/atrium"]
