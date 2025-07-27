FROM golang:1.17-alpine

RUN apk add ca-certificates

WORKDIR /go/src/github.com/xybydy/iptv-proxy
COPY . .
RUN GO111MODULE=off CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o iptv-proxy .

FROM alpine:3
COPY --from=0  /go/src/github.com/xybydy/iptv-proxy/iptv-proxy /
ENTRYPOINT ["/iptv-proxy --m3u-url http://cf.cdn-90.me/get.php?username=e3d5b2a605c2&password=snato6jb00&type=m3u_plus&output=m3u8 --hostname m3u-stream-merger-proxy333.onrender.com --port 80 --xtream-user e3d5b2a605c2 --xtream-password snato6jb00 --xtream-base-url http://cf.cdn-90.me --user test --password passwordtest"]
