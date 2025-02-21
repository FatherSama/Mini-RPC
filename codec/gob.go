/*
	GobCodec 是基于 Gob 编码的编解码器实现。
	它实现了 Codec 接口，提供了基于 Gob 的序列化和反序列化功能。
	具体实现包括：
	- 创建编码器和解码器
	- 实现 ReadHeader 和 ReadBody 方法，用于读取请求头和请求体
	- 实现 Write 方法，用于写入响应
	- 实现 Close 方法，用于关闭编解码器
*/

package codec

import (
	"bufio"      // 提供带缓冲的 I/O 操作
	"encoding/gob"  // Go 语言的二进制序列化格式
	"io"         // 基本的 I/O 接口
	"log"        // 日志功能
)

// GobCodec 结构体封装了 Gob 编解码器所需的所有字段
type GobCodec struct {
	conn io.ReadWriteCloser // 底层的连接对象，用于网络通信
	buf  *bufio.Writer      // 带缓冲的写入器，提升写入性能
	dec  *gob.Decoder       // Gob 解码器，用于解码接收到的数据
	enc  *gob.Encoder       // Gob 编码器，用于编码要发送的数据
}

// 通过空接口赋值确保 GobCodec 实现了 Codec 接口
var _ Codec = (*GobCodec)(nil)

// NewGobCodec 创建一个新的 GobCodec 实例
// conn: 用于网络通信的连接对象
func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)  // 创建带缓冲的写入器
	return &GobCodec{
		conn: conn,               // 保存连接对象
		buf:  buf,                // 保存带缓冲的写入器
		dec:  gob.NewDecoder(conn), // 创建解码器，直接从连接读取
		enc:  gob.NewEncoder(buf),  // 创建编码器，写入到缓冲区
	}
}

// ReadHeader 读取并解码请求头
// h: 用于存储解码后的请求头数据
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// ReadBody 读取并解码请求体
// body: 用于存储解码后的请求体数据，可以是任意类型
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

// Write 将请求头和请求体编码并写入连接
// h: 要编码的请求头
// body: 要编码的请求体，可以是任意类型
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		// 确保缓冲区中的数据被写入连接
		_ = c.buf.Flush()
		// 如果发生错误，关闭连接
		if err != nil {
			_ = c.Close()
		}
	}()

	// 编码并写入请求头
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}

	// 编码并写入请求体
	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}

	return nil
}

// Close 关闭连接，释放资源
func (c *GobCodec) Close() error {
	return c.conn.Close()
}