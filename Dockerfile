# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /app .

# ---- runtime stage (scratch = minimal) ----
FROM scratch
COPY --from=build /app /app
# minimal CA bundle for any outbound TLS; timezone data is embedded in the
# binary itself via time/tzdata (no external zoneinfo needed).
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
EXPOSE 8080
VOLUME ["/data"]
ENV DB_PATH=/data/todo.db
ENV PORT=8080
ENV TZ=Asia/Shanghai
ENTRYPOINT ["/app"]
