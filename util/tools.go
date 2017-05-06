package util

import (
    "os"
    "os/exec"
    "io/ioutil"
)

func RebuildDir(dir string){
    if err := os.RemoveAll(dir);err != nil{
        panic(err)
    }
    if err := os.Mkdir(dir,0774);err != nil{
        panic(err)
    }
}

const Len = 9   //支持几位数字
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