FROM golang:1.21-bookworm

# Install dependencies for building and runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    wireguard-tools \
    iproute2 \
    ca-certificates \
    curl \
    libnss3 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    libpango-1.0-0 \
    libcairo2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Install playwright and chromium
RUN go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps chromium

COPY . .
RUN go build -o mcsnipergo ./cmd/cli

RUN chmod +x mcsnipergo startup.sh

# Create data directory for persistent tokens
RUN mkdir -p /app/data
ENV TOKENS_PATH=/app/data/tokens.json

ENTRYPOINT ["./startup.sh"]