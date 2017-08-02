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
    "os/exec"
    "strings"
    "path/filepath"
    "regexp"
    "strconv"
    "encoding/csv"
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

    InitConfig()

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

func responseError(w http.ResponseWriter,err error) {
    w.WriteHeader(500)
    w.Write([]byte(err.Error()))
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
	if err != nil {   //如果出错，关闭连接
        conn.Close()
		connStat.status = "NOK"
        delete(connMap,conn)   //从节点状态map里删除？
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


//获取节点状态
func getNodeStatus(w http.ResponseWriter, r *http.Request) {

    nodeStatus := make(map[string]string)
    
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

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(body)

    conn, err := net.DialTimeout("tcp",string(body),time.Second*10);
    if  err != nil {
       fmt.Println(err.Error())
       responseError(w,err)
       return  
    }
    
    fmt.Println(conn.LocalAddr())
    fmt.Println(conn.RemoteAddr())

    connMap[conn] = &ConnStat{status:"OK", lock:sync.Mutex{} }
    go CheckStatus(conn)

    w.Write([]byte("OK"))
}

//断开连接
func removeConnect(w http.ResponseWriter, r *http.Request) {
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(body)

    removeAddr := string(body)

    for conn,ConnStat := range connMap{
        if removeAddr == conn.RemoteAddr().String() {
            conn.Close()
            ConnStat.status = "NOK"
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

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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
    fmt.Println(LoadConfig)

    //加载dataConfig.json
    jsonData,_ = ioutil.ReadFile("dataConfig.json")
    if err := json.Unmarshal(jsonData,&dataConfig); err != nil{
        panic(err)
    }
    fmt.Println(dataConfig)

    //加载vardefine.json
    jsonData,_ = ioutil.ReadFile("vardefine.json")
    if err := json.Unmarshal(jsonData,&varDefine); err != nil{
        panic(err)
    }
    fmt.Println(varDefine)

    //加载 export.sql
    exportsql,_ = ioutil.ReadFile("export.sql")
    fmt.Println(string(exportsql))

}

//获取export.sql的内容
func getExportSQL(w http.ResponseWriter, r *http.Request) {
    w.Write(exportsql)
}

//保存 ExportSQL 的配置
func saveExportSQL(w http.ResponseWriter, r *http.Request) {

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))
    exportsql = body

    if err := saveConfig(exportsql,"testExport.sql"); err != nil{
        responseError(w,err)
        return
    }
    w.Write([]byte("OK"))
}

//执行 exportSQL 导出源数据
func executeExportSQL(w http.ResponseWriter, r *http.Request) {

    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    executesql := make(map[string]string)

    if err := json.Unmarshal(body,&executesql); err != nil{
        responseError(w,err)
        return
    }

    fmt.Println(executesql)
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
    fmt.Println( SqlString )

    result := ExecSQLPlus(SqlString)
    fmt.Println( result )

    w.Write([]byte(result))

}


//获取souce目录下源数据目录列表
func getSourceList(w http.ResponseWriter, r *http.Request) {
    fileInfos,_ := ioutil.ReadDir("source")
    var sourceList = make([]string,0)

    for _,v := range fileInfos{
        if(v.IsDir()){
            fmt.Println(v.Name())
            sourceList = append(sourceList,v.Name())
        }
    }

    fmt.Println(sourceList)

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
            fmt.Println(v.Name())
            if _,ok := dataConfig.Models[v.Name()]; !ok{
                dataConfig.Models[v.Name()] = 0
            }
        }
    }

    fmt.Println(dataConfig.Models)
    result,_ := json.Marshal(dataConfig.Models)
    w.Write(result)
}


//保存模板配置
func saveModelConfig(w http.ResponseWriter, r *http.Request) {
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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
    
    fmt.Println(r)
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
    fmt.Println(sourceFiles)

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
                fmt.Println("ERROR: " + varName + " not found. Please check in vardefine.json or RandConfMap.")
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
                        fmt.Println(curVarName +" Grow Automatic: " + curVarStr)
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

        fmt.Println(SourceStr + "\n" + ModelStr)
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
            fmt.Println("WARN:Please check " + i + ":" + v +" manually,maybe it's Wrong.")
        }
    }
}

//删除source或model下的源数据或模板数据
func deleteDir(w http.ResponseWriter, r *http.Request) {
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

    var dirpath = string(body) //目录路径
    if err := os.RemoveAll(dirpath);err != nil{
        responseError(w,err)
        return
    }
    
    w.Write([]byte("OK"))
}

//查看source下的源数据 或 查看 model目录下的模板数据
func checkDetail(w http.ResponseWriter, r *http.Request) {
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

    var dirpath = string(body) //目录路径
    var detail = ""

    err := filepath.Walk(dirpath,func(path string, f os.FileInfo, err error) error{
            if f == nil{
                return err
            }
            if f.IsDir() || !strings.HasSuffix(f.Name(),".unl"){
                return nil
            }

            fmt.Println(path)
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
    fmt.Println(r)
    body, _ := ioutil.ReadAll(r.Body)
    fmt.Println(string(body))

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



/*
    tools  
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
        panic( err )
    }

    cmd.Start()

    _, err = stdin.Write([]byte("set heading off feedback off pagesize 0 verify off echo off numwidth 24 linesize 2000\n"))
    _, err = stdin.Write([]byte(InputSQL))
    if err != nil {
        panic( err )
    }

    stdin.Close()
    content, err := ioutil.ReadAll(stdout)
    if err != nil{
        panic( err )
    }
    return string(content)
}

//将CSV文件解析为数组切片
func ParseCSV(filepath string) ([]string , [][]string, error){
    csvfile,_ := os.Open(filepath)
    csvReader := csv.NewReader(csvfile)
    records,err := csvReader.ReadAll()
    if err != nil{
        fmt.Println("ERROR:" + filepath + " was parsed failed,this file is wrong CSV format. ")
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