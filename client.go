/*
Client 是 RPC 框架的客户端实现。
它实现了远程过程调用的客户端功能，提供了同步和异步两种调用方式。
具体实现包括：
- 创建和管理 RPC 客户端连接
- 提供 Call 方法实现同步调用
- 提供 Go 方法实现异步调用
- 实现请求的编码和发送
- 实现响应的接收和解码
- 管理并发请求和连接状态
*/

package miniRPC

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"miniRPC/codec"
	"net"
	"sync"
)

// Call 表示一个正在进行的 RPC 调用
type Call struct {
	Seq           uint64      // 请求编号，用于区分不同的请求
	ServiceMethod string      // 调用的服务和方法名，格式是 "服务名.方法名"
	Args          interface{} // 调用方法时传入的参数
	Reply         interface{} // 服务端返回的结果
	Error         error       // 如果调用过程中发生错误，这里会记录错误信息
	Done          chan *Call  // 一个 channel，当调用完成时会把结果发送到这里
}

// done 方法用于标记调用完成
// 当 RPC 调用完成时，会把这个 Call 对象发送到 Done channel
func (call *Call) done() {
	call.Done <- call
}

// Client 表示一个 RPC 客户端
// 一个客户端可以同时处理多个调用请求
// 也可以被多个 goroutine 同时使用
type Client struct {
	cc       codec.Codec      // 编解码器，用于对数据进行编码和解码
	opt      *Option          // RPC 客户端的配置选项
	sending  sync.Mutex       // 互斥锁，保护发送请求的过程
	header   codec.Header     // 请求的头部信息
	mu       sync.Mutex       // 互斥锁，保护以下字段
	seq      uint64           // 请求编号，每个请求都有一个唯一的编号
	pending  map[uint64]*Call // 存储未完成的请求，key 是请求编号
	closing  bool             // 表示用户是否已调用 Close 方法关闭客户端
	shutdown bool             // 表示服务端是否已通知客户端停止服务
}

// 确保 Client 实现了 io.Closer 接口
var _ io.Closer = (*Client)(nil)

// 定义一个错误常量，表示连接已关闭
var ErrShutdown = errors.New("connection is shut down")

// Close 关闭客户端连接
func (client *Client) Close() error {
	client.mu.Lock()         // 加锁保护
	defer client.mu.Unlock() // 确保函数结束时解锁

	if client.closing { // 如果已经在关闭中，返回错误
		return ErrShutdown
	}
	client.closing = true    // 标记为正在关闭
	return client.cc.Close() // 关闭编解码器
}

// IsAvailable 检查客户端是否可用
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	// 只有当客户端既没有关闭，也没有被服务端终止时才是可用的
	return !client.shutdown && !client.closing
}

// registerCall 注册一个新的 RPC 调用
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	// 如果客户端正在关闭或已被终止，拒绝注册新的调用
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}

	call.Seq = client.seq           // 分配一个新的请求编号
	client.pending[call.Seq] = call // 将调用保存到待处理映射中
	client.seq++                    // 请求编号自增
	return call.Seq, nil            // 返回请求编号
}

// removeCall 移除并返回指定编号的调用
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq] // 获取调用对象
	delete(client.pending, seq) // 从待处理映射中删除
	return call
}

// terminateCalls 终止所有待处理的调用
// 当发生错误时会调用此方法
func (client *Client) terminateCalls(err error) {
	client.sending.Lock() // 锁定发送操作
	defer client.sending.Unlock()
	client.mu.Lock() // 锁定客户端状态
	defer client.mu.Unlock()

	client.shutdown = true // 标记客户端已被终止
	// 遍历所有待处理的调用，设置错误并标记完成
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// send 发送 RPC 请求
func (client *Client) send(call *Call) {
	// 加锁确保同一时间只有一个请求在发送
	// 防止多个请求的数据混淆在一起
	client.sending.Lock()
	defer client.sending.Unlock()

	// 注册这个调用，获取一个唯一的序列号
	seq, err := client.registerCall(call)
	if err != nil {
		// 如果注册失败（比如客户端正在关闭），
		// 设置错误信息并通过 channel 通知调用完成
		call.Error = err
		call.done()
		return
	}

	// 准备请求头信息
	client.header.ServiceMethod = call.ServiceMethod // 设置要调用的服务和方法名
	client.header.Seq = seq                          // 设置请求序号
	client.header.Error = ""                         // 清空错误信息

	// 将请求编码并发送
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		// 如果发送失败，从待处理列表中移除这个调用
		call := client.removeCall(seq)
		// 如果调用还存在（可能已经被处理完），
		// 设置错误信息并通知调用完成
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// receive 持续接收服务端的响应
func (client *Client) receive() {
	var err error
	// 循环接收响应，直到发生错误
	for err == nil {
		var h codec.Header
		// 读取响应头
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		// 根据序列号找到对应的调用
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			// 如果找不到对应的调用（可能已经被处理完）
			// 直接读取并丢弃响应体
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// 如果响应头中包含错误信息
			// 设置错误并标记调用完成
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// 正常情况：读取响应体到 call.Reply 中
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done() // 标记调用完成
		}
	}
	// 如果循环因错误退出，终止所有待处理的调用
	client.terminateCalls(err)
}

// Go 异步调用，立即返回一个 Call 对象
// serviceMethod：要调用的服务和方法名
// args：调用参数
// reply：用于存储调用结果
// done：可选的 channel，用于接收调用完成的通知
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		// 如果没有提供 done channel，创建一个带缓冲的 channel
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		// done channel 必须是带缓冲的，否则可能会阻塞
		log.Panic("rpc client: done channel is unbuffered")
	}
	// 创建一个新的 Call 对象
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call) // 发送请求
	return call       // 返回 Call 对象
}

// Call 同步调用，等待调用完成并返回错误信息
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	// 使用 Go 方法发起异步调用，然后等待调用完成
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error // 返回调用的错误信息
}

// parseOptions 解析客户端选项
func parseOptions(opts ...*Option) (*Option, error) {
	// 如果没有提供选项，使用默认选项
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	// 只允许提供一个选项
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	// 使用默认的魔数
	opt.MagicNumber = DefaultOption.MagicNumber
	// 如果没有指定编码类型，使用默认的编码类型
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// NewClient 创建一个新的客户端实例
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	// 根据编码类型获取对应的编解码器创建函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	// 将选项信息发送给服务端
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	// 创建客户端实例
	return newClientCodec(f(conn), opt), nil
}

// newClientCodec 创建一个新的客户端编解码器
func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     1,                      // 序列号从 1 开始，0 表示无效调用
		cc:      cc,                     // 设置编解码器
		opt:     opt,                    // 设置选项
		pending: make(map[uint64]*Call), // 创建待处理调用的映射
	}
	// 启动一个 goroutine 接收响应
	go client.receive()
	return client
}

// Dial 连接到指定地址的 RPC 服务器
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	// 解析选项
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	// 建立网络连接
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	// 如果创建客户端失败，确保关闭连接
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}
