# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.1 AS build
WORKDIR /src
ARG APP_VERSION=dev
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
  go build -ldflags "-X memeindex/internal/client.buildVersion=${APP_VERSION}" -o /out/memeindex .

FROM debian:bookworm-slim
WORKDIR /app
ARG APP_VERSION=dev
ARG TARGETARCH
# yt-dlp release to install. Defaults to "latest", which is resolved to a
# concrete tag at build time; pin a dated release (e.g. 2025.08.22) for a
# reproducible image. The downloaded binary is always verified against the
# SHA2-256SUMS file published with that release.
ARG YTDLP_VERSION=latest
ENV MEMEINDEX_VERSION=${APP_VERSION}

RUN set -eux; \
  apt-get update; \
  apt-get install -y --no-install-recommends ca-certificates curl ffmpeg gosu; \
  rm -rf /var/lib/apt/lists/*; \
  case "${TARGETARCH:-amd64}" in \
    amd64) ytdlp_asset=yt-dlp_linux ;; \
    arm64) ytdlp_asset=yt-dlp_linux_aarch64 ;; \
    arm) ytdlp_asset=yt-dlp_linux_armv7l ;; \
    *) echo "unsupported TARGETARCH: ${TARGETARCH:-amd64}" >&2; exit 1 ;; \
  esac; \
  ver="${YTDLP_VERSION}"; \
  if [ "${ver}" = "latest" ]; then \
    ver="$(curl -fsSL https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"; \
  fi; \
  test -n "${ver}"; \
  base="https://github.com/yt-dlp/yt-dlp/releases/download/${ver}"; \
  curl -fsSL "${base}/${ytdlp_asset}" -o /usr/local/bin/yt-dlp; \
  curl -fsSL "${base}/SHA2-256SUMS" -o /tmp/ytdlp-sums; \
  awk -v a="${ytdlp_asset}" '$2 == a { print $1"  /usr/local/bin/yt-dlp" }' /tmp/ytdlp-sums > /tmp/ytdlp-check; \
  test -s /tmp/ytdlp-check; \
  sha256sum -c /tmp/ytdlp-check; \
  chmod +x /usr/local/bin/yt-dlp; \
  rm -f /tmp/ytdlp-sums /tmp/ytdlp-check; \
  echo "installed yt-dlp ${ver} (${ytdlp_asset})"; \
  groupadd --system --gid 10001 memeindex; \
  useradd --system --uid 10001 --gid 10001 --home-dir /app --shell /usr/sbin/nologin memeindex; \
  mkdir -p /app/data; \
  chown -R memeindex:memeindex /app

COPY --from=build --chown=memeindex:memeindex /out/memeindex /usr/local/bin/memeindex
COPY --chown=memeindex:memeindex static ./static
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080

# The container starts as root only so the entrypoint can chown the data
# directory for an existing root-owned bind mount; it then drops to uid 10001
# (gosu) before running the app. Override with `--user`/`user:` to skip that.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -fsS http://localhost:8080/healthz || exit 1

CMD ["memeindex"]
