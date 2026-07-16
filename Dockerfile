FROM node:20-alpine AS frontend-builder

WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend-builder

RUN apk add --no-cache ca-certificates git make

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /app/web/build ./web/build

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o build/axonrouter ./cmd/server

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=backend-builder /app/build/axonrouter .
ENV HOME=/app/data
RUN mkdir -p /app/data
EXPOSE 3777

VOLUME ["/app/data"]

ENTRYPOINT ["./axonrouter"]
