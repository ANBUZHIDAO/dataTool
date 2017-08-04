package main

import (
	"net"
	"fmt"
	"os"
	"encoding/json"
	//"strconv"
	"log"
	"bytes"
	"time"
)

type Message struct{
    Action string
    Ext string
    Content string
    Sequence int 
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

var logBuf bytes.Buffer
var fileLogger,bufLogger *log.Logger

var ModelSlice []string
var randStrMap map[string]*RandStruct

type RandStruct struct{
    Randslice []string
    Index int
}

func (r *RandStruct) GetNext()(string){
    r.Index++
    if(r.Index >= len(r.Randslice)){
        r.Index = 0
    }
    return r.Randslice[r.Index]
}

var LoadConfig []LoadHelper
var dataConfig *DataConfig

type LoadHelper struct{
    Username    string
    Password    string
    TableList   []string
}

type NodeConfig struct{
    NodeAddr    string
    Config      map[string][]string
}

type DataConfig struct{
    GlobalVar   map[string]int
    ColumnMap   map[string][]string
    ExcludeMap  map[string]bool  //使用map判断是否包含在这里面
    RandConfMap map[string][]string
    EnumlistMap map[string][]string
    Models      map[string]int   //模板对应的比重组成的map
    NodeList    []NodeConfig
}



func main(){

	tcpaddr, err := net.ResolveTCPAddr("tcp4","192.168.1.110:4412")
	checkError(err)
	fmt.Println(tcpaddr)

	listener, err := net.ListenTCP("tcp",tcpaddr)
	checkError(err)

	//设置本地文件日志以及缓冲区日志(缓冲区日志为了传输给管理节点)
	logFile,err  := os.OpenFile(time.Now().Format("20060102150405")+".log",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
    defer logFile.Close()
    if err != nil {
        log.Fatalln("open file error !")
    }
	fileLogger = log.New(logFile,"appNode:",log.Lshortfile | log.Ldate | log.Ltime)
	bufLogger = log.New(&logBuf, "appNode: ", log.Lshortfile | log.Ldate | log.Ltime)

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
//这里的解决方法是管理节点 conn.Read不弄成阻塞式的，5s超时，然后超时后主动发送个检查状态的，发送不到，则主动断开连接。
func HanldeConnect(conn net.Conn) {
	fmt.Println("Accepted") 
		
	var buf [102400]byte

	for {
		n, err := conn.Read(buf[0:])   //如果客户端一次性写入超过102400个字符，也只能读取102400个.
		if err != nil {
			return
		}else{
			fmt.Println(string(buf[:n]))
			receive := new(Message)

    		if err := json.Unmarshal(buf[:n],&receive); err != nil{
        		response := &Response{Result:"NOK", Ext:"UnmarshalMessage", Content: err.Error()}
        		respond(conn,response) 
    			continue
    		}

    		if receive.Action == "buildData"{   // buildData Content传表名
    			fmt.Println("Go to BuildData.")
    			//先进行一系列校验，通过之后才真正起协程造数据，然后通知管理节点
    			//go BuildData()

    			response := &Response{Result:"OK", Ext:"log", Content: "Build Data Start."}
    			respond(conn,response) 
    			continue
    		}

    		if receive.Action == "syncConfig"{   // 
    			fmt.Println(receive.Content)
    			fmt.Println(receive.Ext)

    			err := receiveConfig(receive.Ext,receive.Content)

				response := &Response{Result:"OK", Ext:"syncConfig", Content: "syncConfig Success"}
				if err != nil{
					response = &Response{Result:"NOK", Ext:"syncConfig", Content: err.Error()}
				}

    			respond(conn,response) 
    			continue
    		}

    		fmt.Println("Content:" + receive.Content)
    		if logBuf.Len() > 0 {
    			response := &Response{Result:"OK", Ext:"log", Content: logBuf.String()}
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
		fileLogger.Println(err.Error())
		log.Fatalln( err.Error() )
	}
}

func respond(conn net.Conn, response *Response) {
	reply, _ := json.Marshal(response)

    fmt.Println(string(reply))
    fileLogger.Println(string(reply))

    if _,err := conn.Write(reply); err != nil{
    	fileLogger.Println(err.Error()) //记录错误信息，并关闭连接
    	conn.Close()
    }
}


//保存配置数据
func receiveConfig(Ext string, Content string) (error) {
	switch Ext{
	case "ModelSlice":
    	err := json.Unmarshal([]byte(Content),&ModelSlice)
        return err
    case "randStrMap":
    	err := json.Unmarshal([]byte(Content),&randStrMap)
        return err
    case "dataConfig":
    	err := json.Unmarshal([]byte(Content),&dataConfig)
        return err
    case "LoadConfig":
    	err := json.Unmarshal([]byte(Content),&LoadConfig); 
        return err
        default: return nil
	}
}


