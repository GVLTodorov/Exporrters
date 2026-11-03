# === Build Stage ===
FROM golang:1.24 AS builder

ENV CGO_ENABLED=0

# Set the working directory
WORKDIR /app

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy the source code
COPY . .

# Build the Go application for Linux
RUN go build -o weather-exporter .

# === Run Stage ===
FROM alpine:latest

# Set up necessary CA certificates for HTTP requests
RUN apk --no-cache add ca-certificates

# Set working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/weather-exporter .

# Ensure the binary has execution permissions
RUN chmod +x weather-exporter

# Expose the metrics port
EXPOSE 8080

# Command to run the exporter
CMD  ["./weather-exporter"]
