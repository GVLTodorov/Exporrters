# Use the official Golang image as the base
FROM golang:1.24 AS builder

# Set the working directory
WORKDIR /app

# Copy the Go modules files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the Go application
RUN go build -o weather-exporter main.go

# Use a smaller base image for the final container
FROM alpine:latest

# Set up necessary CA certificates for HTTP requests
RUN apk --no-cache add ca-certificates

# Set the working directory
WORKDIR /root/

# Copy the built executable from the builder stage
COPY --from=builder /app/weather-exporter .

# Expose the metrics port
EXPOSE 8080

# Command to run the exporter with environment variables
CMD ["./weather-exporter"]
