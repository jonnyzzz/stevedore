# Build stage - Using latest Go (1.24) on Alpine Linux (small distro)
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application as a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o stevedore .

# Runtime stage - Using Alpine Linux 3.19 (small distro ~5MB base)
FROM alpine:3.19

WORKDIR /app

# Install required tools: Git, Docker CLI, and Docker Compose
# These tools enable container management and version control within the container
RUN apk add --no-cache \
    ca-certificates \
    git \
    docker-cli \
    docker-cli-compose

# Copy the binary from builder
COPY --from=builder /app/stevedore .

# Run the application
CMD ["./stevedore"]
