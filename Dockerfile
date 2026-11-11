# --- frontend ---
FROM node:22-alpine AS web
WORKDIR /w/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend ./
RUN mkdir -p ../backend/web && npm run build

# --- backend (cgo for SQLite, statically linked) ---
FROM golang:1.22-alpine AS build
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY backend ./
COPY --from=web /w/backend/web ./web
RUN go mod tidy && CGO_ENABLED=1 go build -ldflags '-s -w -linkmode external -extldflags "-static"' -o /loompanel .

# --- runtime: the panel only needs the docker CLI to talk to the host's Docker ---
FROM alpine:3.20
RUN apk add --no-cache docker-cli ca-certificates tzdata
COPY --from=build /loompanel /usr/local/bin/loompanel
ENTRYPOINT ["loompanel"]
