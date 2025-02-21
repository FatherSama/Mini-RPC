// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mini-RPC 实现了一个简单的 RPC 框架
package miniRPC

import (
	"encoding/json" // 用于 JSON 编解码
	"fmt"           // 用于格式化输出
	"io"            // 提供 I/O 原语
	"log"           // 用于日志记录
	"miniRPC/codec" // 自定义的编解码器包
	"net"           // 提供网络操作能力
	"reflect"       // 实现运行时反射
	"sync"          // 提供同步原语
)

// MagicNumber 是 RPC 请求的魔数，用于标识 RPC 请求
const MagicNumber = 0x3bef5c

// Option 定义了 RPC 的选项
type Option struct {
	MagicNumber int        // MagicNumber 标记这是一个 RPC 请求
	CodecType   codec.Type // 客户端可以选择不同的编解码器来编码消息体
}

// DefaultOption 是默认的 RPC 选项
var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType, // 默认使用 Gob 编码
}

// Server 表示一个 RPC 服务器
type Server struct{}

// NewServer 创建一个新的 Server 实例
func NewServer() *Server {
	return &Server{}
}

// DefaultServer 是默认的 Server 实例
var DefaultServer = NewServer()

// ServeConn 在单个连接上运行服务器
// ServeConn 会阻塞，直到客户端断开连接
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }() // 确保连接最终被关闭

	// 解码 Option 信息
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}

	// 验证魔数是否正确
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}

	// 获取编解码器的构造函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	// 使用编解码器处理请求
	server.serveCodec(f(conn))
}

// invalidRequest 是发生错误时响应的占位符
var invalidRequest = struct{}{}

// serveCodec 使用指定的编解码器处理请求
func (server *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex) // 确保完整发送一个响应
	wg := new(sync.WaitGroup)  // 等待所有请求处理完成

	// 循环处理请求
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // 无法恢复，关闭连接
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		// 并发处理请求
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// request 存储调用的所有信息
type request struct {
	h            *codec.Header // 请求头
	argv, replyv reflect.Value // 请求参数和响应值
}

// readRequestHeader 读取请求头
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// readRequest 读取完整的请求
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	// TODO: 目前不知道请求参数的类型
	// 第一天：假设它是字符串类型
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

// sendResponse 发送 RPC 响应
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// handleRequest 处理 RPC 请求
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO: 应该调用注册的 RPC 方法来获取正确的 replyv
	// 第一天：只是打印 argv 并发送一个 hello 消息
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

// Accept 接受监听器上的连接并为每个传入连接提供服务
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		// 为每个连接启动一个新的 goroutine
		go server.ServeConn(conn)
	}
}

// Accept 是一个便捷方法，使用默认服务器接受连接
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }
