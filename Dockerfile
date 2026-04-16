FROM golang:1.26 AS build
WORKDIR /src
ARG APP_VERSION=dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X memeindex/internal/client.buildVersion=${APP_VERSION}" -o /out/memeindex .

FROM debian:bookworm-slim
WORKDIR /app
ARG APP_VERSION=dev
ENV MEMEINDEX_VERSION=${APP_VERSION}

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/memeindex /usr/local/bin/memeindex
COPY static ./static

EXPOSE 8080

CMD ["memeindex"]
