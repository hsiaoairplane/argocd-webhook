# Build the webhook binary in a full Go toolchain image
FROM golang:1.26 AS builder

# Set the working directory
WORKDIR /app

# Cache module downloads before copying the rest of the source
COPY go.mod go.sum ./
RUN go mod download

# Copy the Go source code
COPY . .

# Build a static binary so it can run on a minimal base image
RUN CGO_ENABLED=0 go build -o webhook .

# Run on a minimal, non-root base image
FROM gcr.io/distroless/static:nonroot

# Copy the built binary from the builder stage
COPY --from=builder /app/webhook /webhook

# Expose port for webhook server
EXPOSE 8443

# Run as a non-root user
USER nonroot:nonroot

# Run the webhook
ENTRYPOINT ["/webhook"]
