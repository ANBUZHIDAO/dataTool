package main

import (
    "regexp"
    "fmt"
    "io/ioutil"
    "strings"
    "os"
    "path/filepath"
    "./util"
)

var ReplaceSlice = make([]string,0)

//解析table对应的用户名，构建$user.table的替换器
func BuildTableUserRelation(){
    util.InitLoadConfig()

    fmt.Println(util.LoadConfig)
    for _,v := range util.LoadConfig{
        for _,table := range v.TableList{
            ReplaceSlice = append(ReplaceSlice,"$user."+table,v.Username +"."+table)
        }           
    }
}

func main() {

    BuildTableUserRelation()

    AbsPath,_ := filepath.Abs("source")
    util.ExecSQLPlus("create or replace directory WORKSPACE as '"+ AbsPath + "';")

    SqlBytes,_ := ioutil.ReadFile("exportSQL.sql")
    SqlString := string(SqlBytes)
    
    fmt.Println(ReplaceSlice)
    SqlString = strings.NewReplacer(ReplaceSlice...).Replace(SqlString)
    fmt.Println( SqlString )

    re, _ := regexp.Compile(`\$user\.(\w+)`)
    MatchStrs := re.FindAllString(SqlString,-1)    //匹配$user.table，如果还有说明某些表的配置有问题。 
    for _,v := range MatchStrs{
        fmt.Println("ERROR: can't find the username of table " + strings.TrimPrefix(v,"$user.") + " . Please check loadConfig.json. If the tbale not need , Please delete in exportSQL.sql." )
        os.Exit(1)
    }

    content := util.ExecSQLPlus(SqlString)
    fmt.Println( content )
}
