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

var dataConfig = util.InitDataConfig()
var varDefine = make(map[string][2]string)   //变量配置

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
                for _,v := range dataConfig.ColumnMap[tablename]{
                    if v != column{
                        continue          //不等于根值列配置里的列名，继续
                    }

                    if _,ok := dataConfig.AliasMap[tablename + "." + column]; ok {
                        column = dataConfig.AliasMap[tablename + "." + column]     //如果有别名配置，则值改为别名
                    }

                    _,varMatch := varDefine[column];
                    _,RandMatch := dataConfig.RandConfMap[column];
                    if !varMatch && !RandMatch {
                        fmt.Println("ERROR: " + column + " not found. Please check in vardefine.json")
                        os.Exit(1)
                    }

                    for j,record := range RecordsMap[tablename]{
                        if varMatch{
                            varName,preVar := column,column   //变量名初始化为列名，这里变量名对应vardefine.json里的配置
                        
                            if j > 0{
                                varName = column + strconv.Itoa(j)
                                if j > 1{
                                    preVar = column + strconv.Itoa(j-1)
                                }

                                if _,ok := varDefine[varName];!ok{
                                    PreVarStr := varDefine[preVar][0]
                                    loc := re.FindStringIndex(PreVarStr)

                                    var2,_ := strconv.Atoi(PreVarStr[loc[0]:loc[1]])
                                    growth,_ := strconv.Atoi(varDefine[column][1])
                                    curVarStr := PreVarStr[:loc[0]] + strconv.Itoa(var2+growth)
                                    fmt.Println(varName +" Grow Automatic: " + curVarStr)

                                    varDefine[varName] = [2]string{curVarStr,varDefine[column][1]}
                                } 
                            }

                            ValueMap[record[i]] = varDefine[varName][0]+"${" + varName + "}"
                        }

                        if RandMatch{
                            ValueMap[record[i]] = "${" + column + "}"
                        }
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