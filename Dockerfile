# Build stage
FROM golang:1.17-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o iptv-proxy .

# Final minimal image
FROM alpine:3

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/iptv-proxy /iptv-proxy

EXPOSE 80

ENTRYPOINT ["/iptv-proxy"]
CMD ["--m3u-url", "http://cf.cdn-90.me/get.php?username=e3d5b2a605c2&password=snato6jb00&type=m3u_plus&output=m3u8", \
     "--hostname", "m3u-stream-merger-proxy333.onrender.com", \
     "--port", "80", \
     "--xtream-user", "e3d5b2a605c2", \
     "--xtream-password", "snato6jb00", \
     "--xtream-base-url", "http://cf.cdn-90.me", \
     "--user", "test", \
     "--password", "passwordtest"]
