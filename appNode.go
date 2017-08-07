package main

import (
	"net"
	"fmt"
	"os"
	"encoding/json"
	"strconv"
	"log"
	"bytes"
	"time"
    "os/exec"
    "path/filepath"
    "math/rand"
    "strings"
    "io"
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

var ListenAddr = "192.168.1.110:4412"

var appStatus = 0    
//应用节点的状态，0 为初始状态，接收到连接后，状态改为1，
//单状态不为0时再接收到连接请求，报错
//状态为1时收到关闭连接的请求后，状态改为0，继续监听，可以再次处理连接
//状态为1时收到启动作业的请求，检验通过后，状态改为2（启动）
//造完文件改为3（开始导入）
//全部批次完毕后重新改为1

var logBuf = bytes.NewBufferString("")
var LOG *log.Logger

var SliceCap = 1024*1024*50   //Slice大小，管道里的元素的Slice的容量长度cap，当剩余的长度小于30000时，写入WriteCh，由写入线程写入文件。不宜太大。

var BatchQua = 20000000      // 默认的批次构造数量，默认2000万，意思是如果总数是2500万，则会先造2000万导入后覆盖out文件，再造500万
var ModBatch = 1            //每取一个模板的批次数
var TotalQua,Startvalue = 5,0       //总数，起始值

func LoadGlobaleVar(GlobalVar map[string]int) {
    BatchQua = GlobalVar["BatchQua"]
    ModBatch = GlobalVar["ModBatch"]
    TotalQua = GlobalVar["TotalQua"]
    Startvalue = GlobalVar["Startvalue"]

    fmt.Printf("GlobalVar: %d %d %d %d\n",BatchQua,ModBatch,TotalQua,Startvalue) 
    LOG.Printf("GlobalVar: %d %d %d %d\n",BatchQua,ModBatch,TotalQua,Startvalue)   
}

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

var models = make(map[string] (map[string]*MyTemplate))
var maxTemp = make(map[string][2]string)    //用到的模板

type MyTemplate struct{
    Header string
    Content string
    Strslice []string
    Repslice []int      //使用什么替换方式，0为原始，不替换，1为替换变量，2为随机字符串
    Length int          //使用变量替换后的模板长度，用于控制判断是否需要将Bufferstruct压入WriteCh，以写入磁盘。
                        // Length并不能准确计算出模板会有多长，因为有随机字符串以及枚举值的方式
}


func main(){

	tcpaddr, err := net.ResolveTCPAddr("tcp4",ListenAddr)
	if  err != nil{
        panic(err)
    }

    listener, err := net.ListenTCP("tcp",tcpaddr)
	if  err != nil{
        panic(err)
    }

	//设置本地文件日志以及缓冲区日志(缓冲区日志为了传输给管理节点)
	logFile,err  := os.OpenFile("log/appNode" + time.Now().Format("20060102150405")+".log",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
    defer logFile.Close()
    if err != nil {
        panic(err)
    }
    LOG = log.New(io.MultiWriter(logFile,logBuf),"appNode:",log.Lshortfile | log.Ldate | log.Ltime)

    fmt.Println("appNode will Listen:" + ListenAddr)
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
	LOG.Println("Accepted") 
		
	var buf [5242880]byte

	for {
		n, err := conn.Read(buf[0:])   //如果客户端一次性写入超过5M ，也只能读取5M.
		if err != nil {
			return
		}else{
			LOG.Println(string(buf[:n]))
			receive := new(Message)

    		if err := json.Unmarshal(buf[:n],&receive); err != nil{
        		response := &Response{Result:"NOK", Ext:"UnmarshalMessage", Content: err.Error()}
        		respond(conn,response) 
    			continue
    		}

    		if receive.Action == "startTask"{   
    			LOG.Println("Go to startTask.")
    			//先进行一系列校验，通过之后才真正起协程造数据，然后通知管理节点
    			go StartTask()

    			response := &Response{Result:"OK", Ext:"log", Content: "Build Data Task Start."}
    			respond(conn,response) 
    			continue
    		}

    		if receive.Action == "syncConfig"{   // 
    			LOG.Println(receive.Content)
    			LOG.Println(receive.Ext)

    			err := receiveConfig(receive.Ext,receive.Content)

				response := &Response{Result:"OK", Ext:"syncConfig", Content: "syncConfig Success"}
				if err != nil{
					response = &Response{Result:"NOK", Ext:"syncConfig", Content: err.Error()}
				}

    			respond(conn,response) 
    			continue
    		}

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

func respond(conn net.Conn, response *Response) {
	reply, _ := json.Marshal(response)

    LOG.Println(string(reply))

    if _,err := conn.Write(reply); err != nil{
    	LOG.Println(err.Error()) //记录错误信息，并关闭连接
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
    case "models":
        err := json.Unmarshal([]byte(Content),&models); 
        return err
    case "maxTemp":
        err := json.Unmarshal([]byte(Content),&maxTemp); 
        return err
    default: return nil
	}
}


//使用自定义结构体Bufferstruct作为chan中的元素
type Bufferstruct struct{
    filename string
    endFlag  bool
    buf      []byte
}

var complete = make(chan int)
var writeCh = make(chan *Bufferstruct,4)
var buildCh = make(chan *Bufferstruct,4)


func StartTask() {

    LoadGlobaleVar(dataConfig.GlobalVar)

    var thisConfig = make(map[string][]string)

    for _,NodeConfig := range dataConfig.NodeList{
        if ListenAddr == NodeConfig.NodeAddr{
            thisConfig = NodeConfig.Config
        }
    }

    for dir,_ := range thisConfig{
        if err := RebuildDir(dir);err != nil{
            return
        }
    }

    var bufStructs [4]*Bufferstruct
    for _,v := range bufStructs{
        v = new(Bufferstruct)
        v.buf = make([]byte,0,SliceCap )
        buildCh <- v
    }

    startTime := time.Now()
    Endvalue := Startvalue + TotalQua

    for from,to:= Startvalue,Startvalue + BatchQua; from < Endvalue; from,to = from + BatchQua,to + BatchQua {
        if (to > Endvalue){
            to = Endvalue
        }
        t0 := time.Now()
        threadAmount := 0
        for i,v := range thisConfig{
            go buildBytes(i,v,from,to)
            threadAmount++
        }
        go bufferToFile(threadAmount)

        <-complete

        t1 := time.Now()
        fmt.Printf("This Batch cost time  =%v, Begin to Load Data.\n",t1.Sub(t0))
        //LoadData()
    }

    LoadendTime := time.Now()
    fmt.Printf("Total data created and load cost time  =%v\n",LoadendTime.Sub(startTime))

}

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


func bufferToFile(ThreadCount int) {
    var filemap= make(map[string]*os.File)

    for {
        Chanvalue := <- writeCh
        if Chanvalue.endFlag{
            ThreadCount--
            Chanvalue.endFlag = false  //不用写文件，要把标志重新置为false,否则下一个批次会造不完就异常退出
            buildCh <- Chanvalue     //不用写文件，也要压回构造管道。否则整个管道与线程之间的循环圈可能会元素不够导致死锁
            if ThreadCount <= 0{ //表明构造字符串的线程已经全部结束了
                complete <- 1          //通知主线程，写入文件的线程已经处理完所有字符串。
                break
            }else{   //说明还有其他构造字符串的线程在运行中，本线程需要继续运行
                continue  
            } 
        }

        filename := Chanvalue.filename
        if _,ok := filemap[filename]; !ok{
            temp,err := os.OpenFile( filename,os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
            filemap[filename] = temp
            defer filemap[filename].Close()
            if err != nil {   
                LOG.Println(err.Error())
            }
        }

        _,err := filemap[filename].Write( Chanvalue.buf )
        Chanvalue.buf = Chanvalue.buf[0:0]
        if err != nil {   
            LOG.Println(err.Error())
        }

        buildCh <- Chanvalue          
    }

    LOG.Println("WriteFile Thread Exit.")
}

func buildBytes( dirname string,tablelist []string, from int,to int) {
    LOG.Println("from :" + string(Itoa(from)) +" to:" + string(Itoa(to)) )
    var indexM = 0    //模板索引

    tempStruct := <-buildCh 
    valueBytes,m,randString := Itoa(0),0,""
    thisModel := ""

    for _,table := range tablelist {
        
        for i,j := from,0;i < to;i,j = i+1,j+1{
            if(j>= ModBatch){
                j=0
            }

            if(j==0){
                indexM++
                if indexM>=len(ModelSlice){
                    indexM = 0
                }
                thisModel = ModelSlice[indexM]
            }

            thisTemplate,ok := models[thisModel][table]
            if !ok{        //虽然在LoadConfig.json里配置了，但是没有对应的模板
                continue
            } 

            valueBytes = Itoa(i)

            for index,method := range thisTemplate.Repslice{
                m = len(tempStruct.buf)
                if method == 1{
                    tempStruct.buf = tempStruct.buf[0:m+Len]   //原本的版本是使用bytes.Buffer的WriteString。现参考bytes.Buffer的源代码，修改为更底层的调用
                    copy(tempStruct.buf[m:], valueBytes)
                } else if method ==2{
                    randString = randStrMap[thisTemplate.Strslice[index]].GetNext()
                    tempStruct.buf = tempStruct.buf[0:m+len(randString)]
                    copy(tempStruct.buf[m:], randString)
                } else{
                    tempStruct.buf = tempStruct.buf[0:m+len(thisTemplate.Strslice[index])]
                    copy(tempStruct.buf[m:], thisTemplate.Strslice[index])
                }
            }

            if( SliceCap - m <= 30000 ){         //当剩余长度小于30000的时候就写入文件，暂未启用根据thisTemplate.length判断
                tempStruct.filename = filepath.Join(dirname,table+".out") 
                writeCh <- tempStruct              
                tempStruct = <-buildCh
            }
        }

        if len(tempStruct.buf) > 0{
            tempStruct.filename = filepath.Join(dirname,table+".out")
            writeCh <- tempStruct              
            tempStruct = <-buildCh
        }
    }       

    tempStruct.endFlag = true
    writeCh <- tempStruct      
}

var LoadControl = `OPTIONS(DIRECT=Y,SKIP_INDEX_MAINTENANCE=Y)
UNRECOVERABLE
LOAD DATA 
INFILE '${infile}'
APPEND
into table ${username}.${tablename}
fields TERMINATED BY "," optionally enclosed by '"'
(${header})`

var TestControl = `OPTIONS(bindsize=25600000,readsize=25600000,streamsize=25600000,rows=5000)
LOAD DATA 
INFILE '${infile}'
APPEND
into table ${username}.${tablename}
fields TERMINATED BY "," optionally enclosed by '"'
(${header})`
//新版本的LoadData，并行起6个导入协程
func LoadData() {

    if TotalQua <= 500000{              // 50万以下，使用传统路径
        LoadControl = TestControl
    }

    var LoadComplete = make(chan int)
    var loadCh = make(chan string,4)
    var RoutineNumber = 6

    for i:=1;i<= RoutineNumber;i++{
        n := i    //必须引入局部变量，否则下面的logfile编号都是同一个值。 
        go func(){

            err := os.Setenv("NLS_DATE_FORMAT","YYYY-MM-DD hh24:mi:ss")
            err = os.Setenv("NLS_TIMESTAMP_FORMAT","YYYY-MM-DD hh24:mi:ssSSS")
            logfile,err := os.OpenFile("log/load"+ strconv.Itoa(n) +".log",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)   
            if err != nil {   
                panic(err)
            }

            for {
                LoaderCommand,OK := <-loadCh        //获取Load管道里的命令
                if !OK{           //如果没有了，表示已经完了，退出Load
                    LOG.Println("LoadData Goroutine " + strconv.Itoa(n) + " End." )
                    LoadComplete <- 1  
                    break;
                }

                LOG.Println("LoadData Goroutine " + strconv.Itoa(n) + " execute: " + LoaderCommand)

                cmd := exec.Command("sqlldr",LoaderCommand)
                cmd.Stdout = logfile
                if err := cmd.Run(); err != nil{
                    LOG.Println(err)
                }
            }            
 
        }()
    }

    loadCmds := make([]string,0)
    //For循环开始构造Load所需命令和控制文件
    for _,config := range LoadConfig{
        for _,table := range config.TableList{
            LoaderCommand := config.Username + "/" + config.Password +" control=log/"+table+".ctl log=log/"+table+".log"
            
            if _,ok := maxTemp[table]; !ok{
                continue
            }
            header := maxTemp[table][0]

            infile := filepath.Join("out", table+".out")
            LOG.Println(infile)
            if _,err := os.Stat(infile); err != nil {
                continue
            }
            
            rep := strings.NewReplacer("${tablename}",table,"${username}",config.Username,"${header}",header,"${infile}",infile)
            tempctl,err := os.OpenFile("log/"+ table +".ctl",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
            _,err = rep.WriteString(tempctl,LoadControl)    
            if err != nil {   
                panic(err)
            }
            tempctl.Close()

            loadCmds = append(loadCmds,LoaderCommand)   //先放到数组切片里，以便做一些打乱顺序等操作  
        }
    }

    //目前默认6个协程，简单采用随机置换算法打乱LoaderCommand顺序。一般情况下不同用户有不同的表空间数据文件，避免多个协程同时加载数据到同一个表空间
    N := len(loadCmds)
    var rs = rand.New(rand.NewSource(time.Now().UnixNano()))

    for i:=0; i< N; i++{
        tempString := loadCmds[i]
        j := rs.Intn(N)
        loadCmds[i] = loadCmds[j]
        loadCmds[j] = tempString
    }
    //打乱顺序后压入管道
    for i:=0; i< N; i++{
        loadCh <- loadCmds[i]
    }

    close(loadCh)

    for i:=1;i<= RoutineNumber;i++{
        <- LoadComplete
    }

}
