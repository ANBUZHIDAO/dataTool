dataTool
===
# dataTool简介
    一个使用Go语言编写的，用于生成大量性能测试数据的工具，工具的目的快速，方便地构造大批量数据。

    采取的方案是使用系统产生少量数据。以这少量用户数据为模板，利用模板大批量构造CSV格式的数据文件，
    使用SqlLoader导入Oracle数据库。MySQL数据库也有类似的 LOAD 可以导入数据。

    实际使用中造文件的速度取决于磁盘的性能。 本人使用过程中，服务器上磁盘性能较好，
    而且配置了分别输出到两个磁盘上，总的速度达到了 500M/s以上。
    实际上最耗时的步骤是在导入数据到数据库时，MySQL我没有使用过，
    但是Oracle的SqlLoader的速度真的不怎么快，1000多万数据，几十张表，共500多G的数据，
    构造只需要不到20分钟，导入Oracle数据库并重建索引，做完表分析需要几个小时。

    之前未并行SqlLoader导入的情况下，1.8T左右的数据文件，分6个批次导入，构造+导入+重建索引+表分析耗时16个小时左右。

## 1、文件构造主流程简介

![image](https://github.com/ANBUZHIDAO/dataTool/blob/master/picture/dataTool%E6%B5%81%E7%A8%8B%E5%9B%BE%E8%A7%A3.JPG)

主流程在hello.go中，使用了Go语言的goroutine。
初始化几个Bufferstruct，在管道和线程之间组成一个循环圈。  
目前Bufferstruct是4个，因为我个人使用过程中，最多也就2个不同的物理磁盘，只启动过2个buildBytes线程。 4个Bufferstruct足够了。  
Bufferstruct中的buf是byte切片，分配足够的内存，避免运行过程中内存分配,避免GC。  
这种方式构造字符串十分高效，比string join和+高几百倍。

buildBytes 只负责构造字符串，byte切片buf中不断增长，剩余长度小于30000时，将Bufferstruct写入。 ---可能存在很大的模板，超过30K的话，需要调大这个值。。   
bufferToFile 负责将构造好的数据写入文件。全部完成后通知主程序。造文件是一个表文件一个表文件地写入，顺序IO。

buildBytes协程可根据配置启动多个，bufferToFile只有1个。  

buildBytes构造完后将Bufferstruct.endFlag置为true，  
bufferToFile根据接收到的endFlag数量判断是否结束，如果已全部结束，写入一个消息到complete管道，来通知main主程序。
不在buildBytes构造完就通知主程序是有可能这时候还没写到文件。


## 2、配置
主要是3个配置文件，loadConfig.json， dataConfig.json,  vardefine.json  

### loadConfig.json
```json
[
  {
    "Username": "scott",
    "Password": "oracle",
    "OutputDir": "outdir",
    "TableList": [
      "emp",
      "dept"
    ]
  },
  {
    "Username": "testuser",
    "Password": "oracle",
    "OutputDir": "",
    "TableList": [
      "inf_subscriber"
    ]
  }
]
```
很简单，一目了然就是用户名，密码，输出目录，表名  
输出目录可以为""，此情况下默认输出到heelo.go当前目录下的out目录。

### dataConfig.json
```json
{
    "ColumnMap": {
        "inf_subscriber":["sub_id","phone_number","firstname"],
        "dept":["deptno","dname","loc"],
        "emp":["empno","ename"]
    },
    "AliasMap": {
        "tablename.columnname": "aliasname"
    },
    "ExcludeMap":{
        "aliasname":true,
        "columnname":true
    },
    "RandConfMap":{
            "ename":["100","2","3","default"],
            "dname":["10","5","9","chinese"],
            "loc":["100","10","10","province"]
    },
    "EnumlistMap":{
            "province":["Henan","Henan","Shandong","Shandong","Jiangsu","Hubei"]
    },
    "Models":[
        {"Value": "model","Weight": 1},
        {"Value": "model2","Weight": 1},
        {"Value": "model3","Weight": 1}
    ]
}
```
ColumnMap   是配置各个表的列名  
AliasMap    配置列的别名，如果有两个表有相同的列名，需要对其中一个配置别名。  
ExcludeMap  是检测冲突时，可以对其中配置的列不进行检测。  
RandConfMap 配置随机字符串初始化，配置值分别是 初始化数量，最小长度，最大长度，模式 
EnumlistMap 枚举值列表 
Models      配置多个不同类型的模板，Weight是模板所占比重  

### vardefine.json
```json
{
    "deptno":["SV6002000","10"],
    "empno":["1000123","10"],
    "phone_number":["188","1"]
}
```
配置变量值，第二个值是针对多行记录的。
如果有多行记录，则默认在当前配置值的数字值上加上第二个值作为新变量。

## 3、使用

分为3步
```
1、 CSV格式源数据文件  
2、 go run genModel.go   ----  产生模板
3、 go run hello.go -s 1000 -t 5   ----  生成数据并导入   -s startvalue, 变量起始值  -t TotalQua 需要造的记录数
```

CSV原始表数据文件，使用者自己想怎么搞了。。。  
针对Oracle数据库，提供了 exportSQL.sql 使用 UTL_FILE 导出 CSV文件到某个目录。

```
golang没有官方的Oracle 数据库连接驱动，而且现有的使用OCI的方式配置过于麻烦。 本工具只是个造数据的工具。。
采用的方式是执行sqlplus来执行Oracle SQL语句。
在util/tools.go 中有ExecSQLPlus函数。返回标准输出。

dataHrlp.go是调用ExecSQLPlus函数执行exportSQL.sql的帮助工具。使之不必通过其他客户端执行。
当然也可以选择通过PLSQL中执行，来导出CSV文件。

在hello.go中执行检测冲突的语句时，使用前后加特征字符串 ResultStart:'||count(*)||':ResultEnd 的方式，
然后截取中间的部分获得执行结果。

```


## 4、模板的产生
genModel.go产生模板时：
原始内容：
```
source/dept.unl
        deptno,dname,loc,
        "10","ACCOUNTING","NEW YORK",
source/emp.unl
        empno,ename,job,mgr,hiredate,sal,comm,deptno,
        "7782","CLARK","MANAGER","7839","1981-06-09 00:00:00","2450",,"10",
        "7839","KING","PRESIDENT",,"1981-11-17 00:00:00","5000",,"10",
        "7934","MILLER","CLERK","7782","1982-01-23 00:00:00","1300",,"10",

```
生成的模板：
```
model/dept.unl
        deptno,dname,loc,
        SV6002000${deptno},${dname},NEW YORK,
model/emp.unl        
        empno,ename,job,mgr,hiredate,sal,comm,deptno,
        1000123${empno},${ename},MANAGER,1000133${empno1},1981-06-09 00:00:00,2450,,SV6002000${deptno},
        1000133${empno1},${ename},PRESIDENT,,1981-11-17 00:00:00,5000,,SV6002000${deptno},
        1000143${empno2},${ename},CLERK,1000123${empno},1982-01-23 00:00:00,1300,,SV6002000${deptno},
```

主要的变化是deptno根据vardefine.json里配置的变量变为 SV6002000${deptno}。  
这里SV6002000是变量前缀，后面是表示需要替换变量，也即造数据时会替换成值数值。

所有的${var}在构造数据时统一替换为一个值，这个值默认是8位，最多支持造 1亿数据
可修改tools.go里的const Len = 9扩大取值范围。
这样的好处是多次导入时，容易控制取值段。

```
注意这里可能存在BUG，因为模板的产生过程中没有维护一个表字段的关联关系，
而是比如deptno是10，产生模板的时候会将其他表如emp表中值是10的变为SV6002000${deptno}
值长度越小，越容易出现这种问题，所以添加了一个WARN，长度小于4的，加了提示 WARN:Please check "variable" manually,maybe it's Wrong

维护表字段的关联关系太麻烦，而且记录数不一样，表很多的时候太难维护。比如我使用过程中50多张表需要构造，其中的关联关系很难维护。
```


hello.go构造时将模板解析为：
```
    strslice: [ 'SV6002000' 'deptno' ',ACCOUNTING,NEW YORK,' ]
    repslice: [  0            1          0                   ]
```
构造时repslice中对应的值是0就直接复制原始字符串，是1就替换变量。



## 5、SQLLoader导入

当前是默认起6个协程执行SqlLoader并行导入，可调整LoadData函数中 var RoutineNumber = 6的值增大活调小并行数量  
如下面是测试时的一个最终的默认输出（未启动数据库实例，所以 exit status 1）
可以看到只有3个文件的情况下，3，4,5都直接结束未执行导入。
```bash
This Batch cost time  =1.24771ms, Begin to Load Data.
outdir/emp.out
outdir/dept.out
out/inf_subscriber.out
LoadData Goroutine 6 execute: scott/oracle control=log/dept.ctl log=log/dept.log
LoadData Goroutine 1 execute: scott/oracle control=log/emp.ctl log=log/emp.log
LoadData Goroutine 2 execute: testuser/oracle control=log/inf_subscriber.ctl log=log/inf_subscriber.log
LoadData Goroutine 3 End.
LoadData Goroutine 4 End.
LoadData Goroutine 5 End.
exit status 1
LoadData Goroutine 6 End.
exit status 1
LoadData Goroutine 2 End.
exit status 1
LoadData Goroutine 1 End.
Total data created and load cost time  =79.523956ms
oracle@oracle1:~/dataTool> 
```

SqlLoader导入有2种方式，传统路径导入、直接路径导入。两种方式具体的可以自己找资料。

http://docs.oracle.com/database/122/nav/portal_booklist.htm  
Oracle官方文档列表，其中的 Utilities 详细讲述了 SqlLoader。
```
目前默认的直接路径导入的控制文件格式是
OPTIONS(DIRECT=Y,SKIP_INDEX_MAINTENANCE=Y)  
---- DIRECT=Y 表示是直接路径导入，SKIP_INDEX_MAINTENANCE=Y 不维护索引，导入后索引失效，然后统一重建索引。
UNRECOVERABLE    ---- 不产生 redo log和archive log，以提高导入速度。如果导入后没有备份，数据文件就损坏了，无法恢复。
LOAD DATA 
INFILE '${infile}'
APPEND
into table ${username}.${tablename}
fields TERMINATED BY "," optionally enclosed by '"'    ---CSV格式的文件，可能含有需要转义的字符串
(${header})
```

默认如果构造的数量小于50万就使用传统路径，50万以上使用直接路径。

RebuildAndGather.sql
```
导入后，执行 RebuildAndGather.sql 查找出失效的索引，统一重建索引，重建索引时使用并行。
重建完索引后做表分析。
做完表分析之后，执行 alter system flush shared_pool 清空共享池。
```