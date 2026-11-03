# === Build Stage ===
FROM golang:1.25 AS builder

ENV CGO_ENABLED=0

# Set the working directory
WORKDIR /app

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the source code
COPY . .

# Run tests before building the release binary
RUN go test ./...

# Build the Go application for Linux
RUN go build -o mikrotik-exporter .

# === Run Stage ===
FROM alpine:latest

# Set up necessary CA certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Set working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/mikrotik-exporter .

# Ensure the binary has execution permissions
RUN chmod +x mikrotik-exporter

# Expose the metrics port
EXPOSE 8080

# Command to run the exporter
CMD ["./mikrotik-exporter"]
