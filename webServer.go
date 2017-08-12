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
    "os/exec"
    "strings"
    "path/filepath"
    "regexp"
    "strconv"
    "encoding/csv"
    "math/rand"
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
	status int
	lock  sync.Mutex   //使用锁来简单保证一下连接不粘包,统一的sendMessage函数里发送消息前先加锁，函数退出时释放锁
    buf [1024000]byte  //不用每次sendMessage都重新申请内存，但是只有一个的话，多个节点发送时会混乱，保证每个连接有自己的缓冲区
}

var connMap = make(map[net.Conn] *ConnStat)
var logMap = make(map[string]*os.File)

var LOG *log.Logger

func main(){

    //设置日志打印
    logFile,err  := os.OpenFile("log/manServer"+ time.Now().Format("20060102150405")+".log",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
    if err != nil {
        panic(err)
    }
    LOG = log.New(logFile,"ManServer:",log.Lshortfile | log.Ldate | log.Ltime)

    InitConfig()

    http.HandleFunc("/connect", ConnectNode) 
    http.HandleFunc("/removeConnect", removeConnect)
    http.HandleFunc("/getNodeStatus", getNodeStatus) 
    http.HandleFunc("/getNodeList", getNodeList)
    http.HandleFunc("/saveNodeList", saveNodeList) 

    http.HandleFunc("/getLoadConfig", getLoadConfig)
    http.HandleFunc("/saveLoadConfig", saveLoadConfig)

    http.HandleFunc("/getVardefine", getVardefine)
    http.HandleFunc("/getColumnMap", getColumnMap)
    http.HandleFunc("/getRandConfMap", getRandConfMap)
    http.HandleFunc("/saveVardefine", saveVardefine)
    http.HandleFunc("/saveColumnMap", saveColumnMap)
    http.HandleFunc("/saveRandConfMap", saveRandConfMap)

    http.HandleFunc("/getExportSQL", getExportSQL)
    http.HandleFunc("/executeExportSQL", executeExportSQL)
    http.HandleFunc("/saveExportSQL", saveExportSQL)

    http.HandleFunc("/getSourceList", getSourceList)
    http.HandleFunc("/getModelConfig", getModelConfig) 
    http.HandleFunc("/saveModelConfig", saveModelConfig)
    http.HandleFunc("/genModel", genModel)
    http.HandleFunc("/deleteDir", deleteDir)
    http.HandleFunc("/checkDetail", checkDetail)

    http.HandleFunc("/getGlobalVar", getGlobalVar)
    http.HandleFunc("/saveGlobalVar", saveGlobalVar)
    http.HandleFunc("/startBuild", startBuild)

    http.HandleFunc("/getLogDetail", getLogDetail)

    http.Handle("/", http.FileServer(http.Dir("EasyUI")))

    fmt.Println("Server will start and Listen at 8060")
    http.ListenAndServe(":8060", nil)
     
}

func responseError(w http.ResponseWriter,err error) {
    w.WriteHeader(500)
    w.Write([]byte(err.Error()))
}

func sendMessage(conn net.Conn,message *Message)(*Response,error) {
	send, _ := json.Marshal(message)

    connStat,ok := connMap[conn]
    if !ok{
    	LOG.Println("Connect is not existed.")
    	return nil,errors.New("CONNECT_NOT_FOUND")
    }

    if connStat.status < 0 {
    	LOG.Println("Connect is Closed")
    	return nil,errors.New("CONNECT_STATUS_ERROR")
    }

    connStat.lock.Lock()
    defer connStat.lock.Unlock()

	_,err := conn.Write(send);
	if err != nil {   //如果出错，关闭连接
        conn.Close()
        connStat.status = -1
        delete(connMap,conn)   //从节点状态map里删除？
        return nil,err
    }

	conn.SetDeadline(time.Now().Add(time.Duration(5 * time.Second)))
    defer conn.SetDeadline(time.Time{})

    buf := connStat.buf
    //for {
	   n, err := conn.Read(buf[0:])   //如果客户端一次性写入超过buf长度的字符，没读完的话，再次读取会接着读
	   if err != nil {   //如果出错，关闭连接
            conn.Close()
		    connStat.status = -1
            delete(connMap,conn)   //从节点状态map里删除？
		    return nil,err
	   }else{
            //不考虑太复杂的场景
            LOG.Println(string(buf[:n]))
            response := new(Response)

            if err := json.Unmarshal(buf[:n],&response); err != nil{
                LOG.Println(err.Error())
                return nil,nil;
            }

            return response,nil
        }
	//}
}

//管理节点主动检查连接状态,每5s检查一次。 功能不止检查状态，兼职拉取日志。。
//日志打印待进一步优化
func CheckStatus(conn net.Conn) {
	message := &Message{Action:"CheckStatus", Ext:"Connect", Content:"Connect Status" }

    for {
    	
    	response,err := sendMessage(conn,message)

    	filename := conn.RemoteAddr().String() + ".log"
        if _,ok := logMap[filename]; !ok{
            temp,logerr := os.OpenFile( filename,os.O_WRONLY|os.O_CREATE|os.O_APPEND,0664)
            if logerr != nil {   
                LOG.Println(logerr)
                continue
            }

            logMap[filename] = temp
        }
        //sendMessage的 err不为空，说明是发送或者接收响应消息 失败
    	if err != nil{
    		LOG.Println(err)
    		logMap[filename].WriteString( err.Error() )
    		break;
    	}else {
            if response != nil && response.Ext == "log" {
                logMap[filename].WriteString(response.Content)
            }else if response != nil && response.Ext == "status"{
                appStatus,_ := strconv.Atoi(response.Content)
                if connMap[conn].status != appStatus{   //状态发生更改时，更新状态并做一些其他的事。
                    connMap[conn].status = appStatus
                    checkAppNodeStat()
                }    
            }
    	}

		time.Sleep(5 * time.Second)
    }
    
}

//检查App节点的状态，当所有App节点构造导入完毕后，主管理节点启动重建索引和表分析工作，分析完之后更改 BuildStatus 状态为0
func checkAppNodeStat() {
    allAppReady := true
    
    for _,ConnStat := range connMap{
        if ConnStat.status != 1{
            allAppReady = false
        }
    }
    //当所有App节点构造导入完毕后，主管理节点启动重建索引和表分析工作
    if allAppReady{
        RebuildIndexAndGather()
    }
 
}

//获取节点状态
func getNodeStatus(w http.ResponseWriter, r *http.Request) {

    nodeStatus := make(map[string]int)
    
    for conn,ConnStat := range connMap{
        nodeStatus[conn.RemoteAddr().String()] = ConnStat.status
    }

    result,_ := json.Marshal(nodeStatus)
    w.Write(result)
}

//获取节点配置
func getNodeList(w http.ResponseWriter, r *http.Request) {

    result,_ := json.Marshal(dataConfig.NodeList)
    w.Write(result)
}

//保存节点配置
func saveNodeList(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var newNodeList []NodeConfig
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newNodeList); err != nil{
        responseError(w,err)
        return
    }

    dataConfig.NodeList = newNodeList
    fileContent,_ := json.MarshalIndent(dataConfig, ""," ")
    if err := saveConfig(fileContent,"testDataConfig.json"); err != nil{
        responseError(w,err)
        return
    }

    w.Write([]byte("OK"))
}

func ConnectNode(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(body)

    conn, err := net.DialTimeout("tcp",string(body),time.Second*10);
    if  err != nil {
       LOG.Println(err.Error())
       responseError(w,err)
       return  
    }
    
    LOG.Println(conn.LocalAddr())
    LOG.Println(conn.RemoteAddr())

    connMap[conn] = &ConnStat{status:1, lock:sync.Mutex{} }

    message := &Message{Action:"CheckStatus", Ext:"Connect", Content:"Connect Status" }
    response,err := sendMessage(conn,message)

    //sendMessage的 err不为空，说明是发送或者接收响应消息 失败,此时在sendMessage中会关闭并从connMap中删除
    if err != nil{   //连接异常
        responseError(w,err)
        return
    }else if (response == nil){
        responseError(w,errors.New("解析" + conn.RemoteAddr().String() + "的响应消息异常"))
        conn.Close()
        delete(connMap,conn)
        return
    }else if(response.Result != "OK"){
        responseError(w,errors.New(conn.RemoteAddr().String() + "返回异常：" + response.Content))
        conn.Close()
        delete(connMap,conn)
        return
    }
    //只有当第一次检查appStatus成功后才算连接节点成功。
    
    go CheckStatus(conn)
    w.Write([]byte("OK"))
}

//断开连接
func removeConnect(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(body)

    removeAddr := string(body)

    for conn,ConnStat := range connMap{
        if removeAddr == conn.RemoteAddr().String() {
            conn.Close()
            ConnStat.status = -1
            delete(connMap,conn)   //从节点状态map里删除？
        }
    }
    
    w.Write([]byte("OK"))
}

func getLoadConfig(w http.ResponseWriter, r *http.Request) {
    result,_ := json.Marshal(LoadConfig)
    w.Write(result)
}

//保存loadConfig的配置
func saveLoadConfig(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    newLoadConfig := make([]LoadHelper,0)
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newLoadConfig); err != nil{
        responseError(w,err)
        return
    }

    LoadConfig = newLoadConfig
    fileContent,_ := json.MarshalIndent(LoadConfig, ""," ")
    if err := saveConfig(fileContent,"testLoadConfig.json"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}


func getVardefine(w http.ResponseWriter, r *http.Request){

    result,_ := json.Marshal(varDefine)
    w.Write(result)
}

//保存Vardefine的配置
func saveVardefine(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var newVarDefine = make(map[string][]string)   //变量配置
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newVarDefine); err != nil{
        responseError(w,err)
        return
    }

    varDefine = newVarDefine
    fileContent,_ := json.MarshalIndent(varDefine, ""," ")
    if err := saveConfig(fileContent,"testVardefine.json"); err != nil{
        responseError(w,err)
        return
    } 
    w.Write([]byte("OK"))
}

func getColumnMap(w http.ResponseWriter, r *http.Request){

    result,_ := json.Marshal(dataConfig.ColumnMap)
    w.Write(result)
}

func getRandConfMap(w http.ResponseWriter, r *http.Request){

    result,_ := json.Marshal(dataConfig.RandConfMap)
    w.Write(result)
}

//保存ColumnMap的配置
func saveColumnMap(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var newColumnMap = make(map[string][]string)   //列名配置
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newColumnMap); err != nil{
        responseError(w,err)
        return
    }

    dataConfig.ColumnMap = newColumnMap
    fileContent,_ := json.MarshalIndent(dataConfig, ""," ")
    if err := saveConfig(fileContent,"testDataConfig.json"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

//保存 RandConfMap 的配置
func saveRandConfMap(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var newRandConfMap = make(map[string][]string)   //列名配置
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newRandConfMap); err != nil{
        responseError(w,err)
        return
    }

    dataConfig.RandConfMap = newRandConfMap
    fileContent,_ := json.MarshalIndent(dataConfig, ""," ")
    if err := saveConfig(fileContent,"testDataConfig.json"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

func saveConfig(fileContent []byte,filename string)(err error){

    vardefineFile,err := os.OpenFile( filename,os.O_RDWR|os.O_CREATE|os.O_TRUNC,0664)
    if err != nil {
        return err
    }

    _,err = vardefineFile.WriteString( string(fileContent) )
    if err != nil {
        return err
    }

    vardefineFile.Close()
    return nil
}

var varDefine = make(map[string][]string)   //变量配置
var LoadConfig []LoadHelper
var dataConfig *DataConfig
var exportsql   []byte

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


//加载设置,启动WebServer前设置必须正确加载。
func InitConfig() {
    //加载loadConfig.json，
    jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err := json.Unmarshal(jsonData,&LoadConfig); err != nil{
        panic(err)
    }
    LOG.Println(LoadConfig)

    //加载dataConfig.json
    jsonData,_ = ioutil.ReadFile("dataConfig.json")
    if err := json.Unmarshal(jsonData,&dataConfig); err != nil{
        panic(err)
    }
    LOG.Println(dataConfig)

    //加载vardefine.json
    jsonData,_ = ioutil.ReadFile("vardefine.json")
    if err := json.Unmarshal(jsonData,&varDefine); err != nil{
        panic(err)
    }
    LOG.Println(varDefine)

    //加载 export.sql
    exportsql,_ = ioutil.ReadFile("export.sql")
    LOG.Println(string(exportsql))

}

//获取export.sql的内容
func getExportSQL(w http.ResponseWriter, r *http.Request) {
    w.Write(exportsql)
}

//保存 ExportSQL 的配置
func saveExportSQL(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))
    exportsql = body

    if err := saveConfig(exportsql,"testExport.sql"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

//执行 exportSQL 导出源数据
func executeExportSQL(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    executesql := make(map[string]string)

    if err := json.Unmarshal(body,&executesql); err != nil{
        responseError(w,err)
        return
    }

    LOG.Println(executesql)
    //先重建目录，已有的情况下删除整个
    if err := RebuildDir("source/" + executesql["modelname"]); err != nil{
        responseError(w,err)
        return
    }

    //先执行创建替换directory的语句
    AbsPath,_ := filepath.Abs("source/" + executesql["modelname"] )
    ExecSQLPlus("create or replace directory WORKSPACE as '"+ AbsPath + "';")

    SqlBytes,_ := ioutil.ReadFile("exportSQL.sql")
    SqlString := string(SqlBytes)
    
    SqlString = strings.NewReplacer("${ExportSQL}",executesql["sql"]).Replace(SqlString)
    LOG.Println( SqlString )

    result := ExecSQLPlus(SqlString)
    LOG.Println( result )

    w.Write([]byte(result))

}


//获取souce目录下源数据目录列表
func getSourceList(w http.ResponseWriter, r *http.Request) {
    fileInfos,_ := ioutil.ReadDir("source")
    var sourceList = make([]string,0)

    for _,v := range fileInfos{
        if(v.IsDir()){
            LOG.Println(v.Name())
            sourceList = append(sourceList,v.Name())
        }
    }

    LOG.Println(sourceList)

    result,_ := json.Marshal(sourceList)
    w.Write(result)
}

//获取 模板比重配置，处理时删除已不存在的配置，未增加的模板比例权重置为0
func getModelConfig(w http.ResponseWriter, r *http.Request) {
    for modelname,_ := range dataConfig.Models {
        fileInfo,err := os.Stat("model/"+ modelname)
        if err != nil || !fileInfo.IsDir(){
            delete(dataConfig.Models, modelname)
        }
    }

    modelFiles,_ := ioutil.ReadDir("model")

    for _,v := range modelFiles{
        if(v.IsDir()){
            LOG.Println(v.Name())
            if _,ok := dataConfig.Models[v.Name()]; !ok{
                dataConfig.Models[v.Name()] = 0
            }
        }
    }

    LOG.Println(dataConfig.Models)
    result,_ := json.Marshal(dataConfig.Models)
    w.Write(result)
}


//保存模板配置
func saveModelConfig(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var Models =make(map[string]int)   //模板配置
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&Models); err != nil{
        responseError(w,err)
        return
    }

    dataConfig.Models = Models
    fileContent,_ := json.MarshalIndent(dataConfig, ""," ")
    if err := saveConfig(fileContent,"testDataConfig.json"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

//生成模板
func genModel(w http.ResponseWriter, r *http.Request) {
    
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    sourcename := string(body)

    var ValueMap = make(map[string]string)   //保存取到的关键值，打印出来，以便简单人工核对
    var HeaderMap = make(map[string][]string)
    var RecordsMap = make(map[string][][]string)   //保存文件内容

    //解析source目录下源数据，取出关键根值，同时判断根值是否有对应的变量配置
    fileInfos,err := ioutil.ReadDir("source/"+ sourcename)
    if err != nil{
        responseError(w,err)
        return
    }

    var sourceFiles = make([]string,0)
    for _,f := range fileInfos{
        if( !f.IsDir() && strings.HasSuffix(f.Name(),".unl") ){
            sourceFiles = append(sourceFiles,f.Name())
        }
    }
    LOG.Println(sourceFiles)

    for _,fname := range sourceFiles {
        var path = "source/"+ sourcename + "/" + fname

        tablename := strings.TrimSuffix(fname,".unl")
        HeaderMap[tablename],RecordsMap[tablename],err = ParseCSV(path)
        if err != nil{
            responseError(w,err)
            return
        }

        if RecordsMap[tablename] == nil{   //只有文件头，没有内容,从RecordsMap中删除
            delete(RecordsMap,tablename)
            continue
        }

        re, _ := regexp.Compile(`\d+$`)

        for i,column := range HeaderMap[tablename]{
            isConfigedColumn := false
            for _,v := range dataConfig.ColumnMap[tablename]{
                if v == column{
                    isConfigedColumn = true
                    break          
                }
            }

            if !isConfigedColumn{
                continue;   //不在列名配置表里，继续
            }  
                
            varName := tablename+"."+ column
            varConfig,varMatch := varDefine[varName];
            _,randMatch := dataConfig.RandConfMap[varName];

            if !varMatch && !randMatch{
                //既在列名配置里，又没有对应的变量或随机配置
                LOG.Println("ERROR: " + varName + " not found. Please check in vardefine.json or RandConfMap.")
                responseError(w,errors.New("ERROR: " + varName + " not found. Please check in vardefine.json or RandConfMap."))
            }

            for j,record := range RecordsMap[tablename]{

                if varMatch {
                    varStr := varConfig[0]
                    growth,_ := strconv.Atoi(varConfig[1])  //变量增长量

                    //针对形如 SV2303 类的变量配置
                    loc := re.FindStringIndex(varStr)
                    varSeq,_ := strconv.Atoi(varStr[loc[0]:loc[1]]) // 2303
                    varPrefix := varStr[:loc[0]]    //SV

                    curVarName := varName + strconv.Itoa(j)  //当前变量名为类似empno1,empno2的形式
                        
                    if _,ok := varDefine[curVarName]; !ok {
                        curVarStr := varPrefix + strconv.Itoa(varSeq+growth*j)
                        LOG.Println(curVarName +" Grow Automatic: " + curVarStr)
                        ValueMap[record[i]] = curVarStr +"${" + curVarName + "}"
                    }else{    //如果主动配置了 empno2 等，则不需要根据来增长
                        ValueMap[record[i]] = varDefine[curVarName][0] +"${" + curVarName + "}"
                    } 
                }

                if randMatch {
                    ValueMap[record[i]] = "${" + varName + "}"
                } 
            }
 
        }
    }


    RebuildDir("model/" + sourcename)
    
    for tablename,records := range RecordsMap{   //对每个表进行处理
        header := strings.Join(HeaderMap[tablename],",")
        SourceStr,ModelStr := header,header
        for _,record := range records{   //对表中的每行记录进行处理
            SourceStr = SourceStr + "\n" + strings.Join(record,",")
            for i,v := range record{   //处理每行中的各个字段值
                if _,ok := ValueMap[v]; ok{   //值能在根值ValueMap里找到，则替换变量
                    record[i]=ValueMap[v]
                }
                if(strings.ContainsAny(record[i],`,"`)){   //含有需要 转义的字符，标准CSV的转义格式是字段值包含(,)则用双引号括起来，包含(")也用双引号，同时"改为""
                    record[i] = strings.Replace(record[i],`"`,`""`,-1)
                    record[i] = `"` + record[i] + `"`
                }
            }
            ModelStr = ModelStr + "\n" + strings.Join(record,",") 
        }
        SourceStr,ModelStr = SourceStr+"\n",ModelStr+"\n"

        LOG.Println(SourceStr + "\n" + ModelStr)
        //将模板字符串写入文件
        tempfile,err := os.OpenFile("model/" + sourcename +"/"+ tablename+".unl",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
        defer tempfile.Close()
        _,err = tempfile.WriteString(ModelStr)
        if err != nil {   
            responseError(w,err)
            return
        }
    }

    for i,v := range ValueMap{
        if len(i) <= 4{
            LOG.Println("WARN:Please check " + i + ":" + v +" manually,maybe it's Wrong.")
        }
    }
}

//删除source或model下的源数据或模板数据
func deleteDir(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var dirpath = string(body) //目录路径
    if err := os.RemoveAll(dirpath);err != nil{
        responseError(w,err)
        return
    }
    
    w.Write([]byte("OK"))
}

//查看source下的源数据 或 查看 model目录下的模板数据
func checkDetail(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var dirpath = string(body) //目录路径
    var detail = ""

    err := filepath.Walk(dirpath,func(path string, f os.FileInfo, err error) error{
            if f == nil{
                return err
            }
            if f.IsDir() || !strings.HasSuffix(f.Name(),".unl"){
                return nil
            }

            LOG.Println(path)
            detail = detail + f.Name() + "\n"

            body, err := ioutil.ReadFile(path)
            if err != nil {
                return err
            }

            detail = detail + string(body) + "\n"

            return nil
        })

    if err != nil{
        responseError(w,err)
    }
    
    w.Write([]byte(detail))
}

//获取全局变量
func getGlobalVar(w http.ResponseWriter, r *http.Request){

    result,_ := json.Marshal(dataConfig.GlobalVar)
    w.Write(result)
}

//保存全局变量
func saveGlobalVar(w http.ResponseWriter, r *http.Request) {
    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var newGlobalVar =make(map[string]int)   //模板配置
    //保存之前尝试解析，解析出错则返回错误，不保存
    if err := json.Unmarshal(body,&newGlobalVar); err != nil{
        responseError(w,err)
        return
    }

    dataConfig.GlobalVar = newGlobalVar
    fileContent,_ := json.MarshalIndent(dataConfig, ""," ")
    if err := saveConfig(fileContent,"testDataConfig.json"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

//开始构造
func startBuild(w http.ResponseWriter, r *http.Request) {
    checkAppNodeStat()

    if BuildStatus != 0{
       responseError(w,errors.New("正在处理构造任务中，此次请求忽略"))
        return 
    }

    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))
    if string(body) == "true"{
        rebuildIndexflag = true
    }else {
        rebuildIndexflag = false
    }

    var err error = nil
    defer func(error){
        if err != nil{
            BuildStatus = 0 //说明构造任务未成功下发到AppNode。
        }else {
            BuildStatus = 3 //构造任务下发到AppNode了
        }
    }(err)

    //根据配置，根据权重模板切片序列 和 初始化随机串
    ModelSlice := InitModels(dataConfig.Models,1000)
    randStrMap,err := InitRand(dataConfig)
    if err != nil{
        responseError(w,err)
        return
    }

    for modeldir,_ := range dataConfig.Models{
        err := ParseDir(modeldir)     //解析模板
        if err != nil {
            responseError(w,err)
            return
        }
    }

    err = ValidateStartValue()
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(ModelSlice,"ModelSlice")
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(randStrMap,"randStrMap")
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(dataConfig,"dataConfig")
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(LoadConfig,"LoadConfig")
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(models,"models")
    if err != nil{
        responseError(w,err)
        return
    }

    err = syncConfig(maxTemp,"maxTemp")
    if err != nil{
        responseError(w,err)
        return
    }

    message := &Message{Action:"startTask", Ext:"startTask", Content:"startTask" }

    for conn,ConnStat := range connMap{
        if ConnStat.status < 0{
            responseError(w, errors.New(conn.RemoteAddr().String() + " status invalid."))
            return
        }
        response,err := sendMessage(conn, message)
        if err != nil{   //连接异常
            responseError(w,err)
            return
        }else if (response == nil){
            err = errors.New("解析" + conn.RemoteAddr().String() + "的响应消息异常")
            responseError(w,err)
            return
        }else if(response.Result != "OK"){
            err = errors.New(conn.RemoteAddr().String() + "返回异常：" + response.Content)
            responseError(w,err)
            return
        }
    }
    
    w.Write([]byte("OK"))
}

//保存Vardefine的配置
func getLogDetail(w http.ResponseWriter, r *http.Request) {

    LOG.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    LOG.Println(string(body))

    var filename = string(body)   //变量配置

    logFile,err := os.Open(filename)
    if err != nil {
        responseError(w,err)
        return
    }    
    
    logFileInfo,err := logFile.Stat()
    if err != nil {
        responseError(w,err)
        return
    }

    var readbuf []byte

    if logFileInfo.Size() > 50*1024{
        readbuf = make([]byte,50*1024)
        logFile.ReadAt(readbuf,logFileInfo.Size()-50*1024)
    }else {
        readbuf,_ = ioutil.ReadAll(logFile)
    }
    
    w.Write(readbuf)
}


var rs = rand.New(rand.NewSource(time.Now().UnixNano()))

type RandStruct struct{
    Randslice []string
    Index int
}

//根据模板比重，初始化随机序列,n的数量不能太小，比如n=5,只取了5个随机数，是不能得到符合权重的随机序列的
func InitModels(Models map[string]int, n int) ( ModelSlice []string){
    var sum = 0

    //range map的时候是随机的。所以另外声明两个Slice保证有序
    var dSlice []string
    var wSlice []int

    for dir,Weight := range Models{
        if _,err := os.Stat("model/"+ dir); err != nil {  //模板目录存在的才处理
            delete(Models,dir)
            continue
        }
        if(Weight <= 0){
            delete(Models,dir)
            continue
        }
        sum = sum + Weight

        dSlice = append(dSlice,dir)
        wSlice = append(wSlice,sum)          
    }
    
    LOG.Println( Models )

    //经过上面的处理后，比如初始的model:1,model1:2,model2:3 
    //在dirSlice和wSlice中的值分别为model,model1,mode2 1 3 5
    //下面产生1-5之间的随机数，遍历slice，小于当前slice，则认落在当前区间。

    for i:=0;i<n;i++{
        x := rs.Intn( sum )  //获取随机数，根据此随机数落到某个因子范围内，取这个因子的值
        for i,dir := range dSlice{
            if x < wSlice[i]{
                ModelSlice = append(ModelSlice,dir)
                break
            }
        }
    }

    return ModelSlice
}

//randConfig:初始化多少（比如常见姓名是500个，初始化500个姓名，所有姓名字符串从这500个里面取），最小长度，最大长度，模式(0:小写字母,1:大写字母,2:数字,3:字母+数字,4:大小写字母,5:汉字,6:大写开头的字母)
func InitRand(dataConfig *DataConfig) (map[string]*RandStruct,error){
    randConfig := dataConfig.RandConfMap
    EnumMap := dataConfig.EnumlistMap

    var randValueMap = make(map[string]*RandStruct)

    for name,config := range randConfig{
        initsize,_ := strconv.Atoi(config[0])
        randValueMap[name]= &RandStruct{make([]string,initsize,initsize),-1};
        if len(config) == 4 {
            for i:=0;i< initsize; i++{
                randValueMap[name].Randslice[i] = RandString(config[1],config[2],config[3]) 
            }
        }else if len(config) == 2{
            for i:=0;i< initsize; i++{
                Enumlist,ok := EnumMap[config[1]]
                if !ok{
                    return nil, errors.New("枚举值列表" + config[1] + "未配置枚举值");
                }

                randValueMap[name].Randslice[i] = Enumlist[rs.Intn( len(Enumlist) )] 
            }
        }
    }

    return randValueMap,nil
}

//由于是采用的字符串相加的方式，效率不高，此工具采用的是事先初始化好一个不太长的随机串数组，需要用时从数组里循环取
func RandString(min string,max string, mod string) string{

    lowers := "abcdefghijklmnopqrstuvwxyz"
    uppers := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
    digits := "0123456789"
    alnums := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    alphas := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    chinese := "的一是在不了有和人这中大为上个国我以要他时来用们生到作地于出就分对成会可主发年动同工也能下过子说产种面而方后多定行学法所民得经十三之进着等部度家电力里如水化高自二理起小物现实加量都两体制机当使点从业本去把性好应开它合还因由其些然前外天政四日那社义事平形相全表间样与关各重新线内数正心反你明看原又么利比或但质气第向道命此变条只没结解问意建月公无系军很情者最立代想已通并提直题党程展五果料象员革位入常文总次品式活设及管特件长求老头基资边流路级少图山统接知较将组见计别她手角期根论运农指几九区强放决西被干做必战先回则任取据处队南给色光门即保治北造百规热领七海口东导器压志世金增争济阶油思术极交受联什认六共权收证改清己美再采转更单风切打白教速花带安场身车例真务具万每目至达走积示议声报斗完类八离华名确才科张信马节话米整空元况今集温传土许步群广石记需段研界拉林律叫且究观越织装影算低持音众书布复容儿须际商非验连断深难近矿千周委素技备半办青省列习响约支般史感劳便团往酸历市克何除消构府称太准精值号率族维划选标写存候毛亲快效斯院查江型眼王按格养易置派层片始却专状育厂京识适属圆包火住调满县局照参红细引听该铁价严龙飞"

    dicts := map[string]string{"lowers":lowers,"uppers":uppers,"digits":digits,"alnums":alnums,"alphas":alphas,"chinese":chinese}

    min_size,_ := strconv.Atoi(min)
    max_size,_ := strconv.Atoi(max)

    result,n := "",min_size 
    if max_size > min_size {
        n = rs.Intn(max_size-min_size+1)+ min_size
    }

    switch {
    case mod == "lowers" || mod =="uppers" || mod =="digits" || mod =="alnums" || mod =="alphas":
        length := len(dicts[mod])
        for i:=0; i<n; i++{
            result = result + string(dicts[mod][rs.Intn(length)])
        }
    case mod == "chinese":
        length := len(chinese)
        for i:=0; i<n; i++{
            x := rs.Intn(length/3)   
            result = result + chinese[x*3:x*3+3]    //汉字在utf-8里是占用3个byte的
        }
    default :                                     //默认是首字母大写，后面字母小写的方式
        result = string(uppers[rs.Intn(26)])
        for i:=1; i<n; i++{
            result = result + string(lowers[rs.Intn(26)])
        }
    }

    return result
}

//同步配置到App应用节点
func syncConfig(v interface{}, configname string)(error){
    jsonData,_ := json.MarshalIndent(v, ""," ")

    message := &Message{Action:"syncConfig", Ext:configname, Content:string(jsonData) }

    for conn,ConnStat := range connMap{
        if ConnStat.status < 0 {
            return errors.New(conn.RemoteAddr().String() + " status not OK, con't syncConfig.")
        }
        response,err := sendMessage(conn, message)
        if err != nil{ //说明连接异常
            return err
        }else if (response == nil){
            return errors.New("解析" + conn.RemoteAddr().String() + "的响应消息异常")
        }else if(response.Result != "OK"){
            return errors.New(conn.RemoteAddr().String() + "返回异常：" + response.Content)
        }
    }
    
    return nil
}

type MyTemplate struct{
    Header string
    Content string
    Strslice []string
    Repslice []int      //使用什么替换方式，0为原始，不替换，1为替换变量，2为随机字符串
    Length int          //使用变量替换后的模板长度，用于控制判断是否需要将Bufferstruct压入WriteCh，以写入磁盘。
                        // Length并不能准确计算出模板会有多长，因为有随机字符串以及枚举值的方式
}

var models = make(map[string] (map[string]*MyTemplate))
var maxTemp = make(map[string][2]string)    //用到的模板

func ParseDir(dirname string)(error){
    var templates = make(map[string]*MyTemplate)

    err := filepath.Walk("model/"+dirname,func(path string, f os.FileInfo, err error) error{
            if f == nil{
                return err
            }
            if f.IsDir() || !strings.HasSuffix(f.Name(),".unl") {
                return nil
            }

            LOG.Println(path )

            filename := strings.TrimSuffix(f.Name(),".unl")
            //解析文件，读取到字符串里去,然后解析为模板
            data,_ := ioutil.ReadFile(path)
            templates[filename] = parseTemplate(string(data))

            if len(templates[filename].Content) > len(maxTemp[filename][1]){
                maxTemp[filename] = [2]string{templates[filename].Header,templates[filename].Content}
            } 

            return nil
        })

    models[dirname] = templates 

    return err      
}

func parseTemplate(tempStr string)(*MyTemplate){
    var result = new(MyTemplate)

    Header := tempStr[:strings.Index(tempStr, "\n")]
    result.Header = strings.TrimSuffix(Header,",")
    result.Content = tempStr[strings.Index(tempStr, "\n")+1:]
    result.Length = 0

    strArray := strings.Split(result.Content,"${")
    for _,v := range strArray{
        if(!strings.Contains(v,"}")){
            result.Strslice = append(result.Strslice,v)
            result.Repslice = append(result.Repslice,0)
            result.Length = result.Length + len(v)
        } else {
            varName,repMethod := v[:strings.Index(v,"}")],1

            if _,ok := dataConfig.RandConfMap[varName];ok{
                repMethod=2
            }

            result.Strslice = append(result.Strslice,varName)
            result.Repslice = append(result.Repslice,repMethod)
            result.Length = result.Length + 8

            result.Strslice = append(result.Strslice,v[strings.Index(v,"}")+1:])
            result.Repslice = append(result.Repslice,0)
            result.Length = result.Length + len(v[strings.Index(v,"}")+1:])
        }
    }

    fmt.Printf("strSlice =%v\n",result.Strslice)
    fmt.Printf("repSlice =%v\n",result.Repslice)
    result.Length = result.Length + 200   // 由于Length并不能准确计算出模板会有多长，因此将计算出的值增加200，以避免出错。如果出现那种造200以上的随机字符串活枚举字符串之类的，我也只能无语了，改大这个值吧。
    LOG.Println("MyTemplate Length: " + strconv.Itoa(result.Length))

    return result
}

var BuildStatus = 0
//根据要造哪些表，拼接关键根值，查询数据库，如果有冲突则提示并终止
func ValidateStartValue()( error ){
    BuildStatus = 1
    defer func(){BuildStatus = 2}()
    var ValidateString = `select 'ResultStart:'||count(*)||':ResultEnd' from $username.$tablename r where r.$column  between '$from' and '$to';`
    
    Startvalue,TotalQua := dataConfig.GlobalVar["Startvalue"],dataConfig.GlobalVar["TotalQua"]
    v_from,v_to := string(Itoa(Startvalue)),string(Itoa(Startvalue + TotalQua));
    resultReg := regexp.MustCompile("ResultStart:.*:ResultEnd")          //给结果前后加上特定的值，以便于通过正则表达式从SQLPlus执行结果中取出
    
    for _,config := range LoadConfig{
        for _,tablename := range config.TableList{
            username := config.Username

            if !isInNodeList(tablename){
                continue
            }

            if thisTemplate,tok := maxTemp[tablename]; tok{   //在这次构造的模板里的才检查。比如某个表需要多构造一批数据，那么只会对这个表来检查。
                //根据TabConf，找对应模板中的根值变量，组装校验SQL所需的between and的值。
                if _,ok := dataConfig.ColumnMap[tablename]; !ok{  //如果dataConfig.json里没配置，则继续下一个循环
                    continue
                }

                for _,column := range dataConfig.ColumnMap[tablename]{  //只检查dataConfig.json里配置列名，差不多足够了,有的列查起来全表扫描太耗时，如inf_offering_inst的purchase_seq,需要配置在ExcludeColumn里      
                
                    fmt.Println(column)
                    if dataConfig.ExcludeMap[tablename +"."+column]{  //如果包含在ExcludeMap里，则不需要检查这一列。
                        continue
                    }
                    re, _ := regexp.Compile(`\w+\${`+ tablename +`\.`+column + `\d?\},`)   //例子： 匹配如 1899101000${sub_id1},
                    findStrings := re.FindAllString(thisTemplate[1],-1)  //找到所有的匹配，处理可能有多行记录的情况，一般有几行就会匹配到几个

                    for _,v := range findStrings{
                        vardefine := v[:strings.Index(v, "$")]  //例如 1899101000${sub_id},  vardefine=1899101000
                        
                        from,to := vardefine + v_from ,vardefine + v_to 
                        rep := strings.NewReplacer("$tablename",tablename,"$username",username,"$column",column,"$from",from,"$to",to )
                        validateSQL := rep.Replace( ValidateString )

                        fmt.Println(validateSQL)

                        result := resultReg.FindString(ExecSQLPlus(validateSQL))
                        result = strings.TrimPrefix(result,"ResultStart:")
                        result = strings.TrimSuffix(result,":ResultEnd")

                        if result != "0" && result != "'||count(*)||'"{
                            fmt.Println(result)
                            return errors.New(tablename + "中有重复记录, 请检查:" + validateSQL )
                        }
                    }                         
                }
            }
        }
    }

    return nil   
}

//数据结构不好，导致代码这里也就有点难看。。。 暂时先这样--后续改进
func isInNodeList(table string)(bool){

    for _,NodeConfig := range dataConfig.NodeList{
        for _,tablelist := range NodeConfig.Config{
            for _,_table := range tablelist{
                if _table == table {
                    return true
                }
            } 
        }
    }

    return false
}

var rebuildIndexflag bool

func RebuildIndexAndGather(){
    if rebuildIndexflag{
            ReIndexStartTime := time.Now()
            if dataConfig.GlobalVar["TotalQua"] > 500000 {
            LOG.Println( "Begin to Rebuild invalid index and analyse Table" )
            SqlBytes,_ := ioutil.ReadFile("RebuildAndGather.sql")
            SqlString := string(SqlBytes)

            result := ExecSQLPlus(SqlString)
            LOG.Println( result ) 

            ReIndexEndTime := time.Now()
            LOG.Printf("Rebuild Index cost time  =%v\n",ReIndexEndTime.Sub(ReIndexStartTime))
        }
    }

    BuildStatus = 0
}

/*
    tools 工具类 
*/

//重建目录
func RebuildDir(dir string)(error){
    if err := os.RemoveAll(dir);err != nil{
        return err
    }
    if err := os.Mkdir(dir,0774);err != nil{
        return err
    }
    return nil
}

const Len = 8   //支持几位数字
//转数字为字符，比Sprintf高效，且这样容易控制变量长度，可以调整const Len为9位. strconv中的库函数Itoa不足8位时前面无法补0，因此写了这个，数字超过8位时，前面高位被丢弃。
func Itoa(number int)  []byte {
    var a [Len]byte
    for p := Len-1; p >= 0; number,p = (number/10),p-1 {
        a[p] = byte((number % 10) + '0' )
    }
    return a[:]
}

//run SQLPlus as sysdba , return Standard Output
func ExecSQLPlus(InputSQL string )( string){
    cmd := exec.Command("sqlplus","/ as sysdba")
    stdin, err := cmd.StdinPipe()
    stdout,err := cmd.StdoutPipe()
    if err != nil {
        return err.Error()
    }

    cmd.Start()

    _, err = stdin.Write([]byte("set heading off feedback off pagesize 0 verify off echo off numwidth 24 linesize 2000\n"))
    _, err = stdin.Write([]byte(InputSQL))
    if err != nil {
        return err.Error()
    }

    stdin.Close()
    content, err := ioutil.ReadAll(stdout)
    if err != nil{
        return err.Error()
    }
    return string(content)
}

//将CSV文件解析为数组切片
func ParseCSV(filepath string) ([]string , [][]string, error){
    csvfile,_ := os.Open(filepath)
    csvReader := csv.NewReader(csvfile)
    records,err := csvReader.ReadAll()
    if err != nil{
        LOG.Println("ERROR:" + filepath + " was parsed failed,this file is wrong CSV format. ")
        return nil, nil, err
    }

    if len(records) < 2 {
        fmt.Printf("Empty records!\n")
        return nil,nil,nil
    }

    header := records[0]
    contents := records[1:]

    return header,contents,nil
}


