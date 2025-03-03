/*
	codec 包定义了消息的编解码接口，是 RPC 框架的核心组件之一。
	它主要包含以下功能：
	- 定义了请求头（Header）结构，用于存储 RPC 调用的元信息
	- 定义了编解码器（Codec）接口，规范了消息的编解码行为
	- 提供了编解码器的注册机制，支持多种序列化方式（如 Gob、JSON 等）
*/

package codec

import (
	"io"
)

type Header struct {
	ServiceMethod string //ServiceMethod 是服务名和方法名，通常与 Go 语言中的结构体和方法相映射。
	Seq           uint64 //Seq 是请求的序号，也可以认为是某个请求的 ID，用来区分不同的请求。
	Error         string //Error 是错误信息，客户端置为空，服务端如果如果发生错误，将错误信息置于 Error 中。
}

type Codec interface {
	io.Closer                         // 继承 io.Closer 接口，提供 Close() 方法
	ReadHeader(*Header) error         // 读取请求头
	ReadBody(interface{}) error       // 读取请求体
	Write(*Header, interface{}) error // 写入响应
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // not implemented
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
