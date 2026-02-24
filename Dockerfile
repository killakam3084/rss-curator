# Multi-stage build for RSS Curator
# Stage 1: Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /build

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o curator ./cmd/curator

# Stage 2: Runtime stage
FROM alpine:latest

# Install runtime dependencies (sqlite3 for dynamic linking)
RUN apk add --no-cache ca-certificates sqlite-libs

# Create app directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /build/curator /app/curator

# Create data directory for SQLite database
RUN mkdir -p /app/data

# Set permissions
RUN chmod +x /app/curator

# Expose port (if needed for any future features)
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["/app/curator"]

# Default command - can be overridden
CMD ["check"]
