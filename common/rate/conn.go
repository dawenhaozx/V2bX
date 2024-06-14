package rate

import (
	"fmt"
	"net"

	"github.com/juju/ratelimit"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

func NewConnRateLimiter(c net.Conn, l *ratelimit.Bucket) *Conn {
	return &Conn{
		Conn:    c,
		limiter: l,
	}
}

type Conn struct {
	net.Conn
	limiter *ratelimit.Bucket
}

func (c *Conn) Read(b []byte) (n int, err error) {
	c.limiter.Wait(int64(len(b)))
	return c.Conn.Read(b)
}

func (c *Conn) Write(b []byte) (n int, err error) {
	c.limiter.Wait(int64(len(b)))
	return c.Conn.Write(b)
}

// PacketConnCounter 包装了 network.PacketConn 并添加了速率限制
type PacketConnCounter struct {
	network.PacketConn
	limiter *ratelimit.Bucket
}

// NewPacketConnCounter 创建一个新的 PacketConnCounter
func NewPacketConnCounter(conn network.PacketConn, l *ratelimit.Bucket) network.PacketConn {
	return &PacketConnCounter{
		PacketConn: conn,
		limiter:    l,
	}
}

// ReadPacket 从连接中读取数据包，应用速率限制
func (p *PacketConnCounter) ReadPacket(buff *buf.Buffer) (destination M.Socksaddr, err error) {
	// 记录读取前的缓冲区长度
	pLen := buff.Len()
	// 从连接中读取数据包
	destination, err = p.PacketConn.ReadPacket(buff)
	if err != nil {
		return destination, err
	}
	// 等待令牌
	p.limiter.Wait(int64(buff.Len() - pLen))
	return destination, err
}

// WritePacket 向连接写入数据包，应用速率限制
func (p *PacketConnCounter) WritePacket(buff *buf.Buffer, destination M.Socksaddr) error {
	// 获取数据包的长度
	dataLen := int64(buff.Len())
	// 等待令牌
	p.limiter.Wait(dataLen)

	// 检查缓冲区容量是否足够
	if dataLen > int64(buff.Cap()) {
		return fmt.Errorf("buffer overflow: capacity %d, need %d", buff.Cap(), dataLen)
	}

	// 检查缓冲区是否已满
	if buff.IsFull() {
		return fmt.Errorf("buffer is full: capacity %d, current length %d", buff.Cap(), buff.Len())
	}

	// 写入数据包
	return p.PacketConn.WritePacket(buff, destination)
}
