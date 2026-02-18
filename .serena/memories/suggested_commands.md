# Suggested Commands

## Build & Run
- `go build -o livesub ./cmd/livesub` — Build binary
- `go vet ./...` — Static analysis
- `./livesub run configs/config.yaml` — Start the service

## Docker
- `docker build -t livesub .` — Build Docker image
- `docker run -v $(pwd)/configs:/app/configs -p 8899:8899 livesub` — Run container

## Git
- `git config user.name MatchaCake`
- `git config user.email MatchaCake@users.noreply.github.com`
