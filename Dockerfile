# Stage 1: Build the Go app
FROM golang:1.23-alpine AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files first to leverage Docker cache for dependency installation
COPY go.mod go.sum ./

# Download all dependencies (this will be cached unless go.mod or go.sum changes)
RUN go mod tidy

# Copy the source code into the container
COPY . .

# Build the Go app with CGO disabled for static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o comanda .

# Stage 2: Create a smaller image for running the app
FROM alpine:3.18

# Install SSL certificates required for the app to run
RUN apk add --no-cache ca-certificates

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the built Go binary from the builder stage
COPY --from=builder /app/comanda .

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the executable
CMD ["./comanda", "server"]
