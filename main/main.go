package main

import (
	"encoding/json"
	"fmt"
	"log"
	"miniRPC"
	"miniRPC/codec"
	"net"
	"time"
)

// startServer 启动RPC服务器
// addr: 用于传递服务器地址的通道
func startServer(addr chan string) {
	// 选择一个空闲端口监听
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	// 打印服务器启动信息
	log.Println("start rpc server on", l.Addr())
	// 将服务器地址发送到通道
	addr <- l.Addr().String()
	// 开始接受连接请求
	miniRPC.Accept(l)
}

func main() {
	// 创建通道用于接收服务器地址
	addr := make(chan string)
	// 在新的goroutine中启动服务器
	go startServer(addr)

	// 以下代码模拟一个简单的RPC客户端
	// 连接到服务器
	conn, _ := net.Dial("tcp", <-addr)
	// 确保连接最终被关闭
	defer func() { _ = conn.Close() }()

	// 等待服务器启动
	time.Sleep(time.Second)

	// 发送Option信息到服务器
	_ = json.NewEncoder(conn).Encode(miniRPC.DefaultOption)
	// 创建Gob编解码器
	cc := codec.NewGobCodec(conn)

	// 发送请求并接收响应
	for i := 0; i < 5; i++ {
		// 创建请求头
		h := &codec.Header{
			ServiceMethod: "Foo.Sum", // 调用的服务方法名
			Seq:           uint64(i), // 请求序号
		}
		// 发送请求头和请求体
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
		// 读取响应头
		_ = cc.ReadHeader(h)
		// 读取响应体
		var reply string
		_ = cc.ReadBody(&reply)
		// 打印响应结果
		log.Println("reply:", reply)
	}
}
