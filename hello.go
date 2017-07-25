package main

import (
    "net"
    "fmt"
    "os"
    "encoding/json"
    "./util"
)

type Message struct{
    Action string
    Ext string
    Content string
    Sequence int      //计划使用来校验所收到的响应与请求相对应
}

type Response struct{
    Result string
    Ext  string
    Content string
    Sequence int 
}

var appStatus = 0    
//应用节点的状态，0 为初始状态，接收到连接后，状态改为非0，如果此时再接收到连接请求，报错
//收到关闭连接的请求后，状态改为0，继续监听，可以再次处理连接
//收到关闭系统的请求后，关闭应用。

var logger = util.GetLogger()

func main(){

    tcpaddr, err := net.ResolveTCPAddr("tcp4","192.168.1.110:4412")
    checkError(err)
    fmt.Println(tcpaddr)

    listener, err := net.ListenTCP("tcp",tcpaddr)
    checkError(err)

    for {
        conn, err := listener.Accept()
        if err != nil {
            continue
        }
        // 在没有客户端有连接之前 Accept陷入休眠状态，TCP三路握手完毕之后，返回conn

        go HanldeConnect(conn)   //起一个协程处理TCP连接 以支持多线程
    }
}

//这里本来可以采用单线程短连接，简单点，但是我就是想搞成这样的TCP Socket长连接了，来打我啊。
//Socket也是我乐意，采用http或rpc等估计更简单，但是我在学习Socket，我乐意，不爽来打我啊
//处理TCP长连接要深刻认识到一点，TCP是一个没有记录边界的字节流协议。小心粘包或缓冲区过小，一次性读不完。
//这里有个问题，管理节点直接CRTL+C之类的，这里将会长期处于CLOSE_WAIT状态，小应用虽然可以不管。。
//解决方法是 conn.Read不弄成阻塞式的，60s超时，然后超时后主动发送个检查状态的，发送不到，则主动断开连接。
func HanldeConnect(conn net.Conn) {
    fmt.Println("Accepted") 
        
    var buf [1024]byte

    for {
        n, err := conn.Read(buf[0:])   //如果客户端一次性写入超过512个字符，也只能读取512个.
        if err != nil {
            return
        }else{
            fmt.Println(string(buf[:n]))
            receive := new(Message)

            if err := json.Unmarshal(buf[:n],&receive); err != nil{
                fmt.Println(err)
            }

            if receive.Action == "buildData"{   // buildData时，Ext传数量，起始值等入参，Content传表名
                fmt.Println("Go to BuildData.")
                //先进行一系列校验，通过之后才真正起协程造数据，然后通知管理节点
                go util.StartTask()

                response := &Response{Result:"OK", Ext:"log", Content: "Build Data Start."}
                respond(conn,response) 
                continue
            }

            if receive.Action == "syncConfig"{   // buildData时，Ext传数量，起始值等入参，Content传表名
                fmt.Println(receive.Content)
                fmt.Println(receive.Ext)

                filename := receive.Ext
                //修改参数，有配置，有全局变量
                configFile,err := os.OpenFile( filename,os.O_RDWR|os.O_CREATE|os.O_TRUNC,0664)

                _,err = configFile.WriteString( receive.Content )
                configFile.Close()

                response := &Response{Result:"OK", Ext:"syncConfig", Content: "syncConfig Success"}
                if err != nil{
                    response = &Response{Result:"NOK", Ext:"syncConfig", Content: err.Error()}
                }

                respond(conn,response) 
                continue
            }

            fmt.Println("Content:" + receive.Content)
            if logContent := util.GetLogbuf(); len(logContent) > 0 {
                response := &Response{Result:"OK", Ext:"log", Content: logContent}
                respond(conn,response) 
            } else{
                response := &Response{Result:"OK", Ext:"Status", Content: "Status Check "}
                respond(conn,response)
                
            }
            
        }
    }
    
}

func checkError(err error) {
    if err != nil {
        logger.Println(err.Error())
        fmt.Println(err.Error())
    }
}

func respond(conn net.Conn, response *Response) {
    reply, _ := json.Marshal(response)

    fmt.Println(string(reply))

    if _,err := conn.Write(reply); err != nil{
        logger.Println(err.Error()) //记录错误信息，并关闭连接
        conn.Close()
    }
}
