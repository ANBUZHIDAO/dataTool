package util

import (
    "fmt"
    "io/ioutil"
    "encoding/json"
    "os"
)

var Outdir = "out/"         // 默认输出文件目录
var LoadConfig []LoadHelper
var dataConfig *DataConfig
var DirTables = make(map[string][]string)    //输出目录与表的对应关系,即目录1：table1,table2... 
var ModelSlice []string
var randValeMap map[string]*RandStruct

type LoadHelper struct{
    Username    string
    Password    string
    OutputDir   string
    TableList []string
}

type DataConfig struct{
    GlobalVar   map[string]int
    ColumnMap   map[string][]string
    ExcludeMap  map[string]bool  //使用map判断是否包含在这里面
    RandConfMap map[string][]string
    EnumlistMap map[string][]string
    Models      map[string]int   //模板对应的比重组成的map
}

//加载设置,不是主程序，遇到错误抛出，不要终止。由主程序判断错误，决定是否终止。
func InitConfig() (err error) {
    //加载loadConfig.json，
	jsonData,_ := ioutil.ReadFile("loadConfig.json")
    if err = json.Unmarshal(jsonData,&LoadConfig); err != nil{
        return err
    }

    //解析目录与表的对应关系
    fmt.Println(LoadConfig)
    for i,_ := range LoadConfig{
        if LoadConfig[i].OutputDir == ""{       
            LoadConfig[i].OutputDir = Outdir        //如果目录没指定，则使用默认目录           
        } 

        DirTables[LoadConfig[i].OutputDir] = append(DirTables[LoadConfig[i].OutputDir],LoadConfig[i].TableList...)    
    }

    fmt.Println(DirTables)
    //加载dataConfig.json
    jsonData,_ = ioutil.ReadFile("dataConfig.json")
    if err := json.Unmarshal(jsonData,&dataConfig); err != nil{
        return err
    }

    ModelSlice = InitModels(dataConfig.Models,100)

    return nil
}

//根据模板比重，初始化随机序列,n的数量不能太小，比如n=5,只取了5个随机数，是不能得到符合权重的随机序列的
func InitModels(Models map[string]int, n int) ( ModelSlice []string){
    var sum = 0

    //range map的时候是随机的。所以另外声明两个Slice保证有序
    var dSlice []string
    var wSlice []int

    for dir,Weight := range Models{
        if _,err := os.Stat(dir); err != nil {  //模板目录存在的才处理
            delete(Models,dir)
            continue
        }
        sum = sum + Weight

        dSlice = append(dSlice,dir)
        wSlice = append(wSlice,sum)          
    }
    
    fmt.Println( Models )

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

