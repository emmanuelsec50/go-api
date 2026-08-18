# ---- Build stage ----
FROM golang:1.26-alpine AS build

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/chi-router .

# ---- Runtime stage ----
FROM alpine:3.20

# Run as non-root
RUN adduser -D -u 10001 appuser
USER appuser

COPY --from=build /out/chi-router /usr/local/bin/chi-router

EXPOSE 3000
ENTRYPOINT ["chi-router"]
