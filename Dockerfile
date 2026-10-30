FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /clop ./cmd/clop

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /clop /usr/local/bin/clop
WORKDIR /app
# Images, stylesheets, scripts and the standalone HTML tools are mounted
# (or copied) into /app/clop.
COPY clop ./clop
ENV STATIC_DIR=/app/clop LISTEN_ADDR=:8000
EXPOSE 8000
ENTRYPOINT ["clop"]
