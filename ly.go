package main

import (
    "fmt"
    "os/exec"
    "io/ioutil"
    "os"
    "strings"
    "regexp"
)

func main() {

    //先重建目录，已有的情况下删除整个
    RebuildDir("sourcetest")

    SqlBytes,_ := ioutil.ReadFile("testsql.sql")
    SqlString := string(SqlBytes)

    result := ExecSQLPlus(SqlString)
    fmt.Println( result )

    resultReg := regexp.MustCompile("(?s)@@-%%-@@.*@@-%%-@@")  // (?s)的作用是使.能匹配跨行，否则仅仅只能匹配单行
    resultReg.Longest() 
    output := resultReg.FindString(result)
    output = strings.TrimPrefix(output,"@@-%%-@@")
    output = strings.TrimSuffix(output,"@@-%%-@@")
    output = strings.Trim(output," \n")
    fmt.Println( "output:" )
    fmt.Println( output )

    v_tabl_sep := "--------------------"
    //开始解析 ： 以 -------------------- （20个-）分割，然后每行的首部是表名，后面是源数据
    tableRecords := strings.Split(output,v_tabl_sep)
    for _,record := range tableRecords {
        record = strings.TrimLeft(record," \n")
        if len(record)<=1 {
            continue
        }
        tablename := record[:strings.Index(record, "\n")]
        tablecontent := record[strings.Index(record, "\n")+1:]

        tmpfile,err := os.OpenFile("sourcetest/"+ tablename +".unl",os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0664)
        _,err = tmpfile.WriteString(tablecontent)    
        if err != nil {   
            panic(err)
        }
        tmpfile.Close()
    }

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