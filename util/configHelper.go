package util

import (
    "fmt"
    "io/ioutil"
    "encoding/json"
    "os"
)

var Outdir = "out/"         // 默认输出文件目录
var LoadConfig = make([]LoadHelper,0)
var DirTables = make(map[string][]string)    //输出目录与表的对应关系

type LoadHelper struct{
    Username string
    Password string
    OutputDir string
    TableList []string
}

type Factor struct{
    Value string    //目录
    Weight int
}

func InitLoadConfig() ( []LoadHelper) {
	jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err := json.Unmarshal(jsonData,&LoadConfig); err != nil{
        panic(err)
    }

    BuildDirRelation()

    return LoadConfig
}

//解析目录与表的对应关系
func BuildDirRelation(){

    fmt.Println(LoadConfig)
    for i,_ := range LoadConfig{
        if LoadConfig[i].OutputDir == ""{       
            LoadConfig[i].OutputDir = Outdir        //如果目录没指定，则使用默认目录           
        } 

        DirTables[LoadConfig[i].OutputDir] = append(DirTables[LoadConfig[i].OutputDir],LoadConfig[i].TableList...)    //不用检测目录所属key在map中是否存在，如果不存在map[]会返回对应类型的零值
    }

    fmt.Println(DirTables)
}

var dataConfig = new(DataConfig)
type DataConfig struct{
    ColumnMap map[string][]string
    AliasMap map[string]string
    ExcludeMap  map[string]bool  //使用map判断是否包含在这里面
    RandConfMap map[string][5]int //随机方式，初始化长度，最小长度，最大长度，模式(0:lowers,1:uppers,2:digits,3:alnums,4:alphas,5:大写开头的字母)
    Models []Factor
}

func InitDataConfig() *DataConfig {
	jsonData,_ := ioutil.ReadFile("TableConf.json")
    if err := json.Unmarshal(jsonData,&dataConfig); err != nil{
        panic(err)
    }

    return dataConfig
}

func InitModels(n int) (ExistModels []Factor, ModelSlice []string){
    var TotalWeight = 0

    for _,factor := range dataConfig.Models{

        if _,err := os.Stat(factor.Value); err != nil {  //模板目录存在的才处理
                continue
        }
        TotalWeight = TotalWeight + factor.Weight
        factor.Weight = TotalWeight
        ExistModels = append(ExistModels,factor)          
    }
    
    fmt.Println( ExistModels )

    for i:=0;i<n;i++{
        n := rs.Intn( TotalWeight )  //获取随机数，根据此随机数落到某个因子范围内，取这个因子的值
        for _,factor := range ExistModels{
            if n < factor.Weight{
                ModelSlice = append(ModelSlice,factor.Value)
                break
            }
        }
    }

    return ExistModels,ModelSlice
}

