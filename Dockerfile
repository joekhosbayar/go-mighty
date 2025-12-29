# syntax=docker/dockerfile:1

############################
# 1) Build stage
############################
FROM golang:1.25 AS build

WORKDIR /app

# Copy only go.mod/sum first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Build a static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o mighty ./cmd/mighty


############################
# 2) Runtime stage
############################
FROM gcr.io/distroless/base-debian12

WORKDIR /app

# Copy only the binary
COPY --from=build /app/mighty .

# Document the port
EXPOSE 8080

# Drop root privileges
USER nonroot:nonroot

# Run the binary directly (no shell)
ENTRYPOINT ["./mighty"]
