# socks5

一个功能完整的 SOCKS5 / SOCKS5h 代理，同时提供服务端与客户端库。

支持 `CONNECT`、`BIND`、`UDP ASSOCIATE`，并额外支持 **UDP over TCP（v1 / v2）**，
IPv4 / IPv6 / 域名均可使用。

## 特性

- SOCKS5 / SOCKS5h，服务端与客户端双向支持
- `CONNECT` / `BIND` / `UDP ASSOCIATE`
- UDP over TCP v1 / v2（SagerNet 协议，与 sing-box、mihomo、Xray 互通）
- 用户名密码认证（RFC 1929）
- 可指定 UDP 策略：标准 UDP 关联、UoT、两者都开、或全部关闭
- 可自定义 `ProxyDial` / `ProxyListen` / `ProxyListenPacket` 等，便于串联多级代理

## 构建

```bash
go build -o socks5 .
```

## 命令行用法

```bash
./socks5 -h
```

```
socks5 - a SOCKS5 proxy server with full TCP/BIND/UDP support.

Usage:
  ./socks5 [options]

Options:
  -a string
        listen on the address (default ":1080")
  -p string
        password
  -u string
        username
  -udp string
        udp mode: all, associate, uot, off (default "all")

UDP modes (-udp):
  all        accept the UDP ASSOCIATE command and UDP over TCP (default)
  associate  accept the UDP ASSOCIATE command only
  uot        accept UDP over TCP only (v1 and v2)
  off        reject all UDP requests

UDP over TCP:
  As a client, tunnel UDP over TCP by sending CONNECT to one of the magic addresses:
    v1: sp.udp-over-tcp.arpa
    v2: sp.v2.udp-over-tcp.arpa

Examples:
  ./socks5 -a :1080
  ./socks5 -a :1080 -u user -p pass -udp uot
```

| 参数 | 说明 | 默认值 |
| --- | --- | --- |
| `-a` | 监听地址 | `:1080` |
| `-u` | 用户名，留空表示不鉴权 | 空 |
| `-p` | 密码，仅当 `-u` 非空时生效 | 空 |
| `-udp` | UDP 模式：`all` / `associate` / `uot` / `off` | `all` |

示例：

```bash
# 无鉴权，UDP 全开
./socks5 -a :1080

# 用户名密码鉴权，只允许 UDP over TCP
./socks5 -a :1080 -u user -p pass -udp uot

# 完全关闭 UDP
./socks5 -a :1080 -udp off
```

### UDP 模式

| `-udp` | `UDP ASSOCIATE` | `UDP over TCP` |
| --- | --- | --- |
| `all` | ✅ | ✅ |
| `associate` | ✅ | ❌ 回 `0x07 Command not supported` |
| `uot` | ❌ 回 `0x07 Command not supported` | ✅ |
| `off` | ❌ | ❌ |

## 作为库使用

> 包名为 `socks5`，导入路径为 `socks5/pkg`。

### 启动服务端

```go
package main

import (
    "log"
    "os"

    socks5 "socks5/pkg"
)

func main() {
    logger := log.New(os.Stderr, "[socks5] ", log.LstdFlags)
    svc := &socks5.Server{
        Logger: logger,
        UDP:    socks5.UDPAll, // socks5.UDPAssociateOnly / socks5.UDPOverTCPOnly / socks5.UDPDisabled
    }
    svc.Authentication = socks5.UserAuth("user", "pass")

    if err := svc.ListenAndServe("tcp", ":1080"); err != nil {
        logger.Println(err)
    }
}
```

常用的 `Server` 字段：

| 字段 | 说明 |
| --- | --- |
| `Authentication` | 认证方式，`socks5.UserAuth(user, pass)`，为空则不鉴权 |
| `UDP` | UDP 模式，零值等价于 `UDPAll` |
| `Logger` | 错误日志 |
| `Context` | 默认上下文 |
| `HandshakeTimeout` | 握手超时，零值不超时 |
| `ProxyDial` / `ProxyListen` / `ProxyListenPacket` / `ProxyListenBind` | 自定义 TCP / UDP 出口 |
| `ProxyOutgoingListenPacket` | UDP 转发时使用的独立出口 socket |
| `BytesPool` | `io.CopyBuffer` 的缓冲区复用 |

### TCP 客户端

```go
// socks5:// 在本地解析域名，socks5h:// 交给代理解析
d, err := socks5.NewDialer("socks5://user:pass@127.0.0.1:1080")
if err != nil {
    return err
}

conn, err := d.Dial("tcp", "example.com:443")
```

### UDP 客户端

```go
// 标准 SOCKS5 UDP ASSOCIATE
conn, err := d.Dial("udp", "8.8.8.8:53")

// UDP over TCP，设置 Dialer.UDPOverTCP 即可
d := &socks5.Dialer{
    ProxyNetwork: "tcp",
    ProxyAddress: "127.0.0.1:1080",
    UDPOverTCP:   socks5.UDPOverTCPV2, // socks5.UDPOverTCPV1
    Timeout:      5 * time.Second,
}
conn, err := d.Dial("udp", "8.8.8.8:53")
```

返回的连接可按 `net.Conn`（`Read` / `Write`）使用，也可按 `net.PacketConn`（`ReadFrom` / `WriteTo`）使用。

## UDP over TCP 协议

客户端通过向上层代理请求**魔法地址**来开启 UDP over TCP，服务端识别到该地址后，
把这条 TCP 连接当作 UDP 转发通道：

| 版本 | 魔法地址 | 请求头 | 数据帧 |
| --- | --- | --- | --- |
| v1 | `sp.udp-over-tcp.arpa` | 无 | `[Addr][Len u16be][Payload]` |
| v2 | `sp.v2.udp-over-tcp.arpa` | `[IsConnect u8][SocksAddr]` | `IsConnect=1`：`[Len u16be][Payload]`<br>`IsConnect=0`：同 v1 |

- `Addr`（每个数据包内）使用 UoT 自己的地址类型：
  `0x00` IPv4，`0x01` IPv6，`0x02` 域名，后跟地址与 `u16be` 端口。
- 请求头里的 `SocksAddr` 使用**标准 SOCKS5 地址类型**：`0x01` IPv4，`0x03` 域名，`0x04` IPv6。
- `IsConnect=1` 时目标地址在请求头中确定一次，后续每个包只带长度前缀，开销更小。

服务端（`-udp` 为 `all` 或 `uot`）会自动识别并处理；客户端只需按上表收发即可。

## 目录结构

```
.
├── main.go            # 命令行入口
├── go.mod
└── pkg
    ├── server.go      # SOCKS5 服务端
    ├── client.go      # SOCKS5 客户端 Dialer
    ├── simple_server.go
    ├── auth.go        # 认证
    ├── common.go      # 协议常量与编解码
    ├── udp.go         # 标准 UDP ASSOCIATE 连接封装
    ├── udp_netip.go   # netip 版本的 UDP 方法
    └── uot.go         # UDP over TCP v1/v2
```

## 说明

本项目基于 [wzshiming/socks5](https://github.com/wzshiming/socks5) 二次开发，
在其基础上增加了 UDP over TCP v1/v2 支持与 UDP 模式选择。

## 测试结果
./socks-udp-check -proxy 127.0.0.1:4444:u:p -mode all -debug                          [16:40:55]
==================================================
![alt text](image-1.png)
## 测试工具
https://github.com/cqfriend/socks5-udp-checker