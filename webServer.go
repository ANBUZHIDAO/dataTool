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
    "net/http"
    "html/template"
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
	lock  sync.Mutex   //使用锁来简单保证一下连接不粘包,统一的sendMessage函数里发送消息前先加锁，函数退出时释放锁
}

var connMap = make(map[net.Conn] *ConnStat)
var logMap = make(map[string]*log.Logger)

func main(){

/*
	var jsonStruct interface{}
	jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err := json.Unmarshal(jsonData,&jsonStruct); err != nil{
        fmt.Println( err )  //后续需要修改这里，如果json格式不正确，则不能发往appNpde
    }

    message := &Message{Action:"buildData", Ext:"TestExt", Content:string(jsonData) }
    sendMessage(conn, message)


	message := &Message{Action:"syncConfig", Ext:"testSyncConfig.json", Content:string(jsonData) }
    sendMessage(conn, message)
*/
	//time.Sleep(120* time.Second)

    http.HandleFunc("/connect", ConnectNode) 
    http.HandleFunc("/nodeInfo", getNodeList) 
    http.HandleFunc("/getLoadConfig", getLoadConfig)
    http.HandleFunc("/saveLoadConfig", saveLoadConfig)
    http.Handle("/", http.FileServer(http.Dir("EasyUI")))
    http.ListenAndServe(":8060", nil)
    
    http.HandleFunc("/",NotFoundHandler)
}

func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/" {
        http.Redirect(w, r, "index.html", http.StatusFound)
    }

    t, err := template.ParseFiles("template/html/404.html")
    if (err != nil) {
        fmt.Println(err)
    }
    t.Execute(w, nil)
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


type NodeInfo struct{
    Nodeaddr string   // 
    Status   string   //OK NOK
}

func getNodeList(w http.ResponseWriter, r *http.Request) {

    nodeLists := make([]NodeInfo,0)
    
    for conn,ConnStat := range connMap{
        nodeLists = append(nodeLists,NodeInfo{conn.RemoteAddr().String(),ConnStat.status})
    }

    result,_ := json.Marshal(nodeLists)

    w.Write(result)

}

func ConnectNode(w http.ResponseWriter, r *http.Request) {
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(body)

    conn, err := net.DialTimeout("tcp",string(body),time.Second*10);
    if  err != nil {
       fmt.Println(err.Error())
       w.Write([]byte(err.Error()))
       return  
    }
    
    fmt.Println(conn.LocalAddr())
    fmt.Println(conn.RemoteAddr())

    connMap[conn] = &ConnStat{status:"OK", lock:sync.Mutex{} }
    go CheckStatus(conn)

    w.Write([]byte("OK"))
}

type LoadHelper struct{
    Username    string
    Password    string
    OutputDir   string
    TableList []string
}

func getLoadConfig(w http.ResponseWriter, r *http.Request) {

    LoadConfig := make([]LoadHelper,0)
    
    //加载loadConfig.json，
    jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err := json.Unmarshal(jsonData,&LoadConfig); err != nil{
        w.Write([]byte(err.Error()))
        return
    }

    result,_ := json.Marshal(LoadConfig)

    w.Write(result)
}

//保存loadConfig的配置
func saveLoadConfig(w http.ResponseWriter, r *http.Request) {

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(body)

    LoadConfig := make([]LoadHelper,0)
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&LoadConfig); err != nil{
        w.Write([]byte(err.Error()))
        return
    }

    result,_ := json.MarshalIndent(LoadConfig, ""," ")
    fmt.Println(string(result))

    //保存testLoadConfig.json
    loadconfigFile,err := os.OpenFile( "testLoadConfig.json",os.O_RDWR|os.O_CREATE|os.O_TRUNC,0664)
    if err != nil {
        w.Write([]byte(err.Error()))
        return
    }

    loadconfigFile.WriteString( string(result) )
    loadconfigFile.Close()
    
    w.Write([]byte("OK"))
}
