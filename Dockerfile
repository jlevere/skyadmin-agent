FROM golang AS builder

# Accept version as build argument
ARG VERSION=dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build the Go app with static linking (no CGo) and embed version
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=${VERSION}" -o skyportal .

# scratch for the actual running container to make sure its small
FROM scratch

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/skyportal /skyportal

ENV TZ=UTC \
    LOG_LEVEL=info

ENTRYPOINT ["/skyportal"]
