# Build stage - Go on Alpine Linux
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Needed to infer build info from .git during docker build
RUN apk add --no-cache git gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application as a static binary
RUN set -eu; \
	VERSION="dev"; \
	if [ -f VERSION ]; then VERSION="$(tr -d '\r\n' < VERSION)"; fi; \
	GIT_COMMIT="unknown"; \
	if git rev-parse HEAD >/dev/null 2>&1; then GIT_COMMIT="$(git rev-parse HEAD)"; fi; \
	GIT_REMOTE_RAW="unknown"; \
	if git config --get remote.origin.url >/dev/null 2>&1; then GIT_REMOTE_RAW="$(git config --get remote.origin.url)"; fi; \
	GIT_REMOTE="${GIT_REMOTE_RAW%.git}"; \
	case "${GIT_REMOTE}" in \
		git@*:* ) host="${GIT_REMOTE#git@}"; host="${host%%:*}"; path="${GIT_REMOTE#git@*:}"; GIT_REMOTE="${host}/${path}" ;; \
		ssh://* ) GIT_REMOTE="${GIT_REMOTE#ssh://}" ;; \
		https://* ) GIT_REMOTE="${GIT_REMOTE#https://}" ;; \
		http://* ) GIT_REMOTE="${GIT_REMOTE#http://}" ;; \
	esac; \
	case "${GIT_REMOTE}" in */* ) \
		authority="${GIT_REMOTE%%/*}"; rest="${GIT_REMOTE#*/}"; \
		case "${authority}" in *@* ) authority="${authority##*@}"; GIT_REMOTE="${authority}/${rest}" ;; esac; \
	esac; \
	BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
	CGO_ENABLED=1 CGO_CFLAGS="-Doff64_t=off_t -Dpread64=pread -Dpwrite64=pwrite" GOOS=linux go build -trimpath -a \
		-ldflags "-s -w -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.GitRemote=${GIT_REMOTE} -X main.BuildDate=${BUILD_DATE}" \
		-o stevedore .

# Runtime stage - Using Alpine Linux (small distro base)
FROM alpine:3.19

WORKDIR /app

# Install required tools: Git, Docker CLI, and Docker Compose
# These tools enable container management and version control within the container
RUN apk add --no-cache \
    ca-certificates \
    git \
    openssh-client \
    docker-cli \
    docker-cli-compose

# Copy the binary from builder
COPY --from=builder /app/stevedore .

# Run the daemon by default (docker/compose service mode)
CMD ["./stevedore", "-d"]
