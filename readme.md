# Mighty Backend Design
<img width="1000" height="685" alt="image" src="https://github.com/user-attachments/assets/15bd4c43-02a8-4dfc-9ccc-78e47b78ae9b" />

# Stuff i did
`go get -u github.com/rs/zerolog/log`
`go get github.com/redis/go-redis/v9`
`go get github.com/lib/pq`



# Manual Build Process
1. Build the executable
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o mighty ./cmd/mighty`
2. Run the server
`./mighty`

# DockerFile Verification
`docker build -t mighty .`
`docker run -p 8080:8080 mighty`

# How to run API and Redis
1. Set up the Docker secrets by creating the password file:
   ```
   cp secrets/postgres_password.txt.example secrets/postgres_password.txt
   ```
2. Update `secrets/postgres_password.txt` with your secure password
3. Start the services:
   ```
   docker compose up
   ```

Note: The application uses Docker secrets for secure credential management. The password is read from `/run/secrets/postgres_password` inside the containers.

# How to run all unit tests
`go test ./...`

# Note on Depends on field in docker yaml
It only guarantees the postgres and redis containers are started before the API starts. Just because the postgres and redis containers are started doesn't always mean they are ready to accept traffic!!

# To reset secrets, need to run docker compose down -v
`docker compose down -v`
Otherwise the container will have an old secret cached.

# Debugging

The debug compose setup will start **all** services defined in both `docker-compose.yml`
and `docker-compose.debug.yml` (API, Postgres, Redis, etc.).

1. (Recommended) Stop any existing stack for this project to avoid port conflicts:

   ```bash
   docker compose down
   ```

2. Start the services in debug mode:

   ```bash
   docker compose -f docker-compose.yml -f docker-compose.debug.yml up --build
   ```

3. The Go debugger will be available on **port 2345** on your host (e.g. `localhost:2345`).

   - In VS Code, the project includes a ready-to-use debug configuration in `.vscode/launch.json`
     named **"Connect to server"** that connects to `localhost:2345`.
   - After the containers are up, select this configuration from the Run and Debug panel and
     start debugging.


# Websocket upgrade request
```
curl --include \
     --no-buffer \
     --header "Connection: Upgrade" \
     --header "Upgrade: websocket" \
     --header "Host: localhost:8080" \
     --header "Origin: http://localhost:8080" \
     --header "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     --header "Sec-WebSocket-Version: 13" \
     http://localhost:8080/games/1234/ws
```