# V2bX

基于 sing-box 的 V2board 节点服务端，由 [dawenhaozx/V2bX](https://github.com/dawenhaozx/V2bX) 维护。

仅维护 sing-box 内核适配，支持 VMess、VLESS、Shadowsocks、Trojan、TUIC、AnyTLS、Hysteria 和 Hysteria2。支持单进程对接多个节点、多个具名 sing 实例、动态用户增删、流量上报、在线 IP 限制和 TCP 限速。

面板继续使用兼容的 UniProxy API，包括 `config`、`user`、`aips`、`push` 和 `alive`。此次迁移不修改面板协议。

## 内核与依赖

内核来自 [dawenhaozx/sing-box 的 testing 分支](https://github.com/dawenhaozx/sing-box/tree/testing)，`go.mod` 固定到已审计的提交 `bdb9628408d199319b608d9504374b81f826694d`，确保构建可以复现。该 fork 保留 V2bX 所需的动态用户管理接口；`sing`、`sing-vmess`、`sing-quic`、`sing-tun` 等继续使用其上游依赖。

Go 的内核导入路径仍是 `github.com/sagernet/sing-box`，由 `replace` 指向自己的 fork。V2bX 的内部 Go module 路径保留历史名称 `github.com/InazumaV/V2bX`，发布和更新均使用本仓库。

## 安装与更新

Linux 安装脚本和 systemd 服务由本仓库维护，支持安装最新 release 或指定版本。需要 root、systemd，以及 `curl`、`unzip`、`sha256sum`。

```bash
release_url=$(curl -fLsS -o /dev/null -w '%{url_effective}' https://github.com/dawenhaozx/V2bX/releases/latest)
version=${release_url##*/}
curl -fL "https://github.com/dawenhaozx/V2bX/releases/download/$version/install.sh" -o install.sh
sudo bash install.sh --version "$version"
```

首次安装后，编辑 `/etc/V2bX/config.json` 中的面板地址、密钥和节点信息，再手动启动服务：

```bash
sudo systemctl enable --now V2bX
```

更新保留 `/etc/V2bX` 中的配置和证书，下载包经过 SHA256 校验。程序和安装器从同一个 release 下载：

```bash
sudo V2bX update
sudo V2bX update --version v1.0.0
```

指定版本必须已发布对应的安装器和校验文件。未发布本次改造的 release 前，请使用本地构建；旧 release 的内容不会因本地修改自动更新。

## 本地构建

需要 Go 1.25.5 或更高版本。

```bash
GOEXPERIMENT=jsonv2 go build -o V2bX \
  -tags "with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" \
  -trimpath -ldflags "-X github.com/InazumaV/V2bX/cmd.version=local -s -w" .
```

sing-box 默认包含在程序中，无需指定 `sing` 内核标签。`with_quic` 用于 Hysteria、Hysteria2 和 TUIC，正式构建应保留上述功能标签。

```bash
GOEXPERIMENT=jsonv2 go test \
  -tags "with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" \
  -timeout 180s ./...
```

普通测试使用本地服务，不连接实际面板或 ACME。需要实际环境的旧集成测试通过 `integration` 构建标签单独启用。

## 配置

参考 [example/config.json](example/config.json) 和 [example/sing_origin.json](example/sing_origin.json)。首次手动安装需将两份文件放入 `/etc/V2bX`，按实际路径修改 `OriginalPath`。

- `Cores` 至少配置一个实例。`Cores[].Type`、`Nodes[].Core` 为 `sing`，省略这两个字段时默认使用 sing。其他内核类型会明确报错。
- 多个 sing 实例使用 `Cores[].Name` 命名，并通过 `Nodes[].CoreName` 选择。
- `EnableTFO` 开启 TCP Fast Open；`TCPMultiPath` 开启 MPTCP，两者默认关闭。MPTCP 还需要操作系统和网络支持。
- 原生 DNS、路由、出站和规则集在 `OriginalPath` 指向的 sing-box 配置中设置。旧 DNS `address`、`dns.fakeip`、旧入站 `sniff`/`domain_strategy` 等字段需要迁移为新版格式。
- 保留历史 `EnableDNS`、`EnableSniff`、`DomainStrategy` 配置字段的解析兼容性，但这些旧开关尚未接入当前适配器；请使用原生配置表达相关规则。UDP 限速、动态限速的既有功能缺口另行维护。

Xray 和独立 Hysteria2 内核的配置、构建和依赖已移除。Hysteria/Hysteria2 协议由 sing-box 提供。

## 项目来源

项目源自 V2bX/XrayR，保留原项目许可证。感谢 [InazumaV/V2bX](https://github.com/InazumaV/V2bX)、[wyx2685/V2bX](https://github.com/wyx2685/V2bX)、[XrayR](https://github.com/XrayR/XrayR) 和 [sing-box](https://github.com/SagerNet/sing-box) 的工作。
