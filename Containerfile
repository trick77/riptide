# Built with the repo root as context (`docker build -f Containerfile .`), as
# ship release does. Hence the backend/ prefix on every source path here.

# The UI, built first: the Go stage embeds its output, so it has to exist on
# disk before `go build` reads it.
FROM node:26-alpine AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ui/.npmrc ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.27-alpine AS builder
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# vite writes into backend/web/dist (ui/vite.config.ts), which in this stage
# is /build/web/dist. Copied AFTER the source, or the COPY above would put
# the tracked .gitkeep back over it.
COPY --from=ui /backend/web/dist ./web/dist
# ship release passes VERSION.
ARG VERSION=dev
# Static binary, no cgo: distroless/static carries nothing but the CA bundle.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/trick77/ship/kit/version.Version=${VERSION}" \
    -o /out/riptide ./cmd/riptide

# 65532, never 1000: 1000 is a Debian host's first user, so a chown for the
# container would hand files to a real account. OpenShift overrides the uid
# with its own random one in group 0; the binary needs no writable path.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=builder /out/riptide /riptide
USER 65532:65532
# Soft heap ceiling under the pod's 512Mi limit: the runtime collects harder
# as it approaches this. Raise it together with the limit.
ENV GOMEMLIMIT=400MiB
EXPOSE 8000
# No shell or wget in distroless: the binary probes its own /healthz, which
# answers loopback callers only.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/riptide", "-healthcheck"]
ENTRYPOINT ["/riptide"]
CMD ["serve"]
