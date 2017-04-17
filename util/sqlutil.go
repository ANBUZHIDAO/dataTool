package util

import (
    "os/exec"
    "io/ioutil"
)


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