# Mighty Backend Engine 🃏🚀
A high-performance, real-time backend for the Mighty card game, built with Go, Redis, and PostgreSQL.

## 🚀 Key Features
- **Authoritative Engine**: Full implementation of Mighty rules, including special card power shifts (Mighty/Joker/Ripper).
- **Social Lobby**: Authenticated matchmaking lobby for game discovery.
- **Bi-Directional WebSockets**: Sub-millisecond move latency via real-time reactive streams.
- **UCLA Scoring**: Accurate implementation of campus-standard scoring and multipliers.
- **Optimistic Concurrency**: Version-tracked state updates to prevent race conditions.

## 🛠️ Manual Build Process
1. **Build the executable**:
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o mighty ./cmd/server
   ```
2. **Run the server**:
   ```bash
   ./mighty
   ```

## 🐳 Docker Deployment
1. **Setup Secrets**:
   ```bash
   cp secrets/postgres_password.txt.example secrets/postgres_password.txt
   # Edit secrets/postgres_password.txt with 'mightypassword'
   ```
2. **Launch Stack**:
   ```bash
   docker compose up -d
   ```

## 📡 WebSocket Handshake
```bash
# Handshake requires an authenticated JWT token
curl --include \
     --no-buffer \
     --header "Connection: Upgrade" \
     --header "Upgrade: websocket" \
     --header "Host: localhost:8080" \
     "http://localhost:8080/games/{id}/ws?token=<jwt_token_here>"
```

## 🧪 Testing
- **Unit Tests**: `go test ./internal/...`
- **E2E Gherkin Features**: `go test -v ./tests/e2e/...`

## 📘 Documentation
- [API Reference](./docs/API_DOCUMENTATION.md)
- [Game Rules](./docs/rules.md)
- [Architecture](./docs/SERVICE_ARCHITECTURE.md)