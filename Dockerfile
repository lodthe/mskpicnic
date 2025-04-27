# Dockerfile was generated from
# https://github.com/lodthe/dockerfiles/blob/main/go/Dockerfile

FROM golang:1.24-alpine3.21 AS builder

# Setup base software for building an app.
RUN apk update && apk add ca-certificates git gcc g++ libc-dev binutils

WORKDIR /opt

# Download dependencies.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy application source.
COPY . .

# Build the application.
# Assuming the main package is in the root directory
RUN go build -o bin/application main.go

# Prepare executor image.
FROM alpine:3.21 AS runner

RUN apk update && apk add ca-certificates libc6-compat openssh bash sqlite && rm -rf /var/cache/apk/* # Added sqlite package

WORKDIR /opt

COPY --from=builder /opt/bin/application ./

# Add required static files.
# COPY assets assets # No assets needed for this app yet

# Run the application.
# The command will be specified in docker-compose.yml
CMD ["./application"]