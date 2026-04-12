# ============================================
# Stage 1: Build Go binary
# ============================================
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agent-forge ./cmd/agent-forge/

# ============================================
# Stage 2: Build React frontend
# ============================================
FROM node:22-alpine AS web-builder

WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --production=false

COPY web/ .
RUN npm run build

# ============================================
# Stage 3: Final image
# ============================================
FROM alpine:3.20

RUN apk add --no-cache \
    git \
    bash \
    curl \
    openssh-client \
    ca-certificates \
    && rm -rf /var/cache/apk/*

# Install Node.js + Claude Code CLI
RUN apk add --no-cache nodejs npm && \
    npm install -g @anthropic-ai/claude-code

# Create app user
RUN addgroup -g 1000 agentforge && \
    adduser -u 1000 -G agentforge -s /bin/bash -D agentforge

# Copy Go binary
COPY --from=go-builder /agent-forge /usr/local/bin/agent-forge

# Copy frontend build
COPY --from=web-builder /app/web/dist /app/web/dist

# Create data directories
RUN mkdir -p /data/.agent-forge && \
    chown -R agentforge:agentforge /data

# Set environment
ENV AGENT_FORGE_HOME=/data/.agent-forge
ENV NODE_ENV=production

# Expose port
EXPOSE 8080

USER agentforge
WORKDIR /data

ENTRYPOINT ["agent-forge"]
CMD ["serve", "--host", "0.0.0.0", "--port", "8080"]
