# Build go
FROM golang:1.22.0-alpine AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=0
RUN go mod download \
&& go build -v -o V2bX -trimpath -tags "sing with_gvisor with_quic with_dhcp with_wireguard with_ech with_utls with_reality_server with_acme with_clash_api"

# Release
FROM  alpine
# 安装必要的工具包
RUN  apk --update --no-cache add curl tzdata ca-certificates \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && mkdir /etc/V2bX/
COPY --from=builder /app/V2bX /usr/local/bin

ENTRYPOINT [ "V2bX", "server", "--config", "/etc/V2bX/config.json"]
