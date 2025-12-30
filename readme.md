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