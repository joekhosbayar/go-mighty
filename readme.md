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
`docker compose up`

# How to run all unit tests
`go test ./...`

# Note on Depends on field in docker yaml
It only guarantees the postgres and redis containers are started before the API starts. Just because the postgres and redis containers are started doesn't always mean they are ready to accept traffic!! 