package main

import (
    "fmt"
    "os"
    "encoding/csv"
    "io/ioutil"
    "encoding/json"
    "strconv"
    "strings"
    "path/filepath"
    "regexp"
    "./util"
)

var dataConfig *util.DataConfig
var varDefine = make(map[string][]string)   //变量配置

//将CSV文件解析为数组切片
func ParseCSV(filepath string) ([]string , [][]string ){
    csvfile,_ := os.Open(filepath)
    csvReader := csv.NewReader(csvfile)
    records,err := csvReader.ReadAll()
    if err != nil{
        fmt.Println("ERROR:" + filepath + " was parsed failed,this file is wrong CSV format. ")
        os.Exit(1)
    }

    if len(records) < 2 {
        fmt.Printf("Empty records!\n")
        return nil,nil
    }

    header := records[0]
    contents := records[1:]

    return header,contents
}

var ValueMap = make(map[string]string)   //全局变量，保存取到的关键值，打印出来，或保存到文件，以便简单人工核对
var HeaderMap = make(map[string][]string)
var RecordsMap = make(map[string][][]string)   //全局变量，保存文件内容
func GetKeyValue(dir string) {
    err := filepath.Walk(dir,func(path string, f os.FileInfo, err error) error{
            if f == nil{
                return err
            }
            if f.IsDir() || !strings.HasSuffix(f.Name(),".unl"){
                return nil
            }

            fmt.Println(path)

            tablename := strings.TrimSuffix(f.Name(),".unl")
            HeaderMap[tablename],RecordsMap[tablename] = ParseCSV(path)
            if RecordsMap[tablename] == nil{   //只有文件头，没有内容,从RecordsMap中删除
                delete(RecordsMap,tablename)
                return nil
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
                    os.Exit(1)
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

            return nil
        })

    if err != nil{
        fmt.Printf("GetKeyValue from source failed!\n")
    }

}

func main() {
    
    jsonData,_ := ioutil.ReadFile("vardefine.json")
    if err := json.Unmarshal(jsonData,&varDefine); err != nil{
        panic(err)
    }

    //加载dataConfig.json
    jsonData,_ = ioutil.ReadFile("dataConfig.json")
    if err := json.Unmarshal(jsonData,&dataConfig); err != nil{
        panic(err)
    }

    //在加载完变量配置后，解析source目录下源数据，取出关键根值，同时判断根值是否有对应的变量配置
    GetKeyValue("source")
    fmt.Println(ValueMap)
	
	util.RebuildDir("model")
    
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
        tempfile,err := os.OpenFile("model/"+ tablename+".unl",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
        defer tempfile.Close()
        _,err = tempfile.WriteString(ModelStr)
        if err != nil {   
            panic(err)
        }
    }

    for i,v := range ValueMap{
        if len(i) <= 4{
            fmt.Println("WARN:Please check " + i + ":" + v +" manually,maybe it's Wrong.")
        }
    }
}