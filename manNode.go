package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"encoding/json"
	"net"
	"time"
	"errors"
	"sync"
	"log"
)

type Message struct{
    Action string
    Ext string
    Content string 
}

type Response struct{
    Result string
	Ext  string
    Content string
}

type ConnStat struct{
	status string // OK NOK
	lock  sync.Mutex   //使用锁来简单保证一下连接不粘包
}

var connMap = make(map[net.Conn] *ConnStat)
var logMap = make(map[string]*log.Logger)

func main(){

	conn, err := net.Dial("tcp","192.168.1.110:4412")
	checkError(err)
	fmt.Println(conn.LocalAddr())
	fmt.Println(conn.RemoteAddr())

	connMap[conn] = &ConnStat{status:"OK", lock:sync.Mutex{} }

	go CheckStatus(conn)

	var jsonStruct interface{}
	jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err := json.Unmarshal(jsonData,&jsonStruct); err != nil{
        fmt.Println( err )  //后续需要修改这里，如果json格式不正确，则不能发往appNpde
    }

    message := &Message{Action:"buildData", Ext:"TestExt", Content:string(jsonData) }
    sendMessage(conn, message)


	message = &Message{Action:"syncConfig", Ext:"testSyncConfig.json", Content:string(jsonData) }
    sendMessage(conn, message)

	time.Sleep(120* time.Second)
}


func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
		os.Exit(1)
	}
}

func sendMessage(conn net.Conn,message *Message)([]byte,error) {
	send, err := json.Marshal(message)
    checkError(err)
    fmt.Println(string(send))

    connStat,ok := connMap[conn]
    if !ok{
    	fmt.Println("Connect is not existed.")
    	return []byte("Connect Error"),errors.New("CONNECT_NOT_FOUND")
    }

    if connStat.status != "OK"{
    	fmt.Println("Connect is Closed")
    	return []byte("Connect Error"),errors.New("CONNECT_STATUS_ERROR")
    }

    connStat.lock.Lock()
    defer connStat.lock.Unlock()

	_,err = conn.Write(send);
	checkError(err)

	fmt.Println("Begin Read Response")
	conn.SetDeadline(time.Now().Add(time.Duration(5 * time.Second)))

	var buf [1024]byte
	n, err := conn.Read(buf[0:])   //如果客户端一次性写入超过buf长度的字符，没读完的话，再次读取会接着读
	if err != nil {   //如果出错只更新状态，客户端选择是否重新连接或者选择删除连接？
		connStat.status = "NOK"
		return buf[:0],err
	}else{
		conn.SetDeadline(time.Time{})
		return buf[:n],nil
	}
}

//管理节点主动检查连接状态,每10s检查一次。 功能不止检查状态，兼职拉取日志。。
//日志打印待进一步优化
func CheckStatus(conn net.Conn) {
	message := &Message{Action:"CheckStatus", Ext:"Connect", Content:"Connect Status" }

    for {
    	
    	response,err := sendMessage(conn,message)

    	filename := conn.RemoteAddr().String() + ".log"
        if _,ok := logMap[filename]; !ok{
            temp,logerr := os.OpenFile( filename,os.O_WRONLY|os.O_CREATE|os.O_APPEND,0664)
            if logerr != nil {   
                fmt.Println(logerr)
                continue
            }

            logMap[filename] = log.New(temp,"appNode:",log.Ldate | log.Ltime)
        }

    	if err != nil{
    		fmt.Println(err)
    		logMap[filename].Println( err.Error() )
    		break;
    	}else {
    		logMap[filename].Println( string(response) )
    		fmt.Println(string(response))
    	}

		time.Sleep(10* time.Second)
    }
    

}

