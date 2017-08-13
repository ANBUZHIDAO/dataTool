dataTool
===
# dataTool简介
    一个使用Go语言编写的，用于生成大量性能测试数据的工具，工具的目的快速，方便地构造大批量数据。

    采取的方案是使用系统产生少量数据。以这少量用户数据为模板，利用模板大批量构造CSV格式的数据文件，
    使用SqlLoader导入Oracle数据库。MySQL数据库也有类似的 LOAD 可以导入数据。

    实际使用中造文件的速度取决于磁盘的性能。 本人使用过程中，服务器上磁盘性能较好，
    而且配置了分别输出到两个磁盘上，总的速度达到了 500M/s以上。
    
    实际上最耗时的步骤是在导入数据到数据库时，MySQL我没有使用过，
    Oracle的SQLLoader导入，以及导入后重建索引是比较耗时的地方。

    这是第二个版本。最大的变化是提供了前台界面，使用多节点一起构造数据。
    实际性能测试基本使用RAC数据库，为充分利用RAC双机的性能和磁盘空间，将构造任务下发到2个节点上去。
    

## 1、dataTool系统简介
节点管理
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/dataTool%E7%B3%BB%E7%BB%9F.png)
```
管理节点状态：
        0: "初始状态",
        1: "冲突校验中",
        2: "冲突校验完毕",
        3: "构造任务下发成功",
        4: "App节点构造导入完毕，开始表分析",  ---- 心跳检测时，会更新appNode的节点状态
        5: "构造任务完毕",                ----- 表分析失败可以从界面获取表分析SQL，修改后在PLSQL中手工执行
        -1: "失败原因,具体设置-1状态的地方修改具体原因信息"
应用节点状态：
        0: 初始状态，接收到连接后，状态改为1，
        1: 正常连接状态
        2：构造数据中
        3：批次构造完成，开始导入导入完成后改为2
状态不为0时，接收到新的连接请求时，如果最后接收到管理节点的消息未超过11秒，则认为已经有连接存在，拒绝新的连接        
状态不为1时，拒绝接受另一个新的构造任务
状态为1时收到启动作业的请求，检验通过后，状态改为2
全部批次完毕后重新改为1
```

文件构造主流程简介

![image](https://github.com/ANBUZHIDAO/dataTool/blob/master/picture/dataTool%E6%B5%81%E7%A8%8B%E5%9B%BE%E8%A7%A3.JPG)
```
主流程在appNode.go的StartTask中，使用Go语言的goroutine。
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
```

## 2、使用

分为3步
```
1、 将工具包上传到RAC节点上去  
2、 go run webServer.go   ----  其中一个节点上启动，作为管理节点
3、 go run appNode.go     ----  两个RAC节点都执行，作为应用节点
```

## 3、配置界面说明
用户配置
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E7%94%A8%E6%88%B7%E9%85%8D%E7%BD%AE.png)

表列配置
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E8%A1%A8%E5%88%97%E9%85%8D%E7%BD%AE.png)

导出数据
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/export.png)

模板配置
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E6%A8%A1%E6%9D%BF%E9%85%8D%E7%BD%AE.png)

节点配置
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E8%8A%82%E7%82%B9%E9%85%8D%E7%BD%AE.png)

启动构造
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E5%90%AF%E5%8A%A8%E6%9E%84%E9%80%A0.png)

查看日志
![image](https://github.com/ANBUZHIDAO/dataTool/blob/TwoNode/picture/%E6%97%A5%E5%BF%97%E6%9F%A5%E7%9C%8B.png)


## 4、后台配置文件简介
主要是3个配置文件，loadConfig.json， dataConfig.json,  vardefine.json  


### loadConfig.json
```json
[
  {
    "Username": "scott",
    "Password": "oracle",
    "TableList": [
      "emp",
      "dept"
    ]
  },
  {
    "Username": "testuser",
    "Password": "oracle",
    "TableList": [
      "inf_subscriber"
    ]
  }
]
```
很简单，一目了然就是用户名，密码，表名  

### dataConfig.json
```json
{
    "GlobalVar": {
        "BatchQua": 200000,
        "ModBatch": 100,
        "Startvalue": 1000,
        "TotalQua": 100
    },
    "ColumnMap": {
        "dept": ["deptno", "dname", "loc"],
        "emp": ["empno", "ename"],
        "inf_subscriber": ["sub_id", "phone_number", "firstname"]
    },
    "ExcludeMap": {
        "aliasname": true,
        "columnname": true
    },
    "RandConfMap": {
        "dept.dname": ["1000", "5", "9", "chinese"],
        "dept.loc": ["100", "province"],
        "emp.ename": ["100", "2", "3", "default"]
    },
    "EnumlistMap": {
        "province": ["Henan", "Henan", "Shandong", "Shandong", "Jiangsu", "Hubei"]
    },
    "Models": {
        "prepaid1": 1, "prepaid2": 1, "prepaid3": 1
    },
    "NodeList": [{
        "NodeAddr": "192.168.1.110:4412",
        "Config": {
            "out": [],
            "out2": []
        }
    },
    {
        "NodeAddr": "192.168.1.111:4412",
        "Config": {
            "/home/oracle/out": ["emp25", "inf_dept"],
            "/opt/out": ["emp24", "inf_subscriber"]
        }
    }]
}
```
ColumnMap   是配置各个表的列名  
ExcludeMap  是检测冲突时，可以对其中配置的列不进行检测。  
RandConfMap 配置随机字符串初始化，配置值分别是 初始化数量，最小长度，最大长度，模式 
EnumlistMap 枚举值列表 
Models      配置多个不同类型的模板，Weight是模板所占比重  

### vardefine.json
```json
{
    "dept.deptno":["SV6002000","10"],
    "inf_subscriber.sub_id":["SV6002000","10"],
    "emp.empno":["1000123","10"],
    "inf_subscriber.phone_number":["188","1"],
    "dept.empno":["1000123","10"]
}
```
配置变量值，第二个值是针对多行记录的。
如果有多行记录，则默认在当前配置值的数字值上加上第二个值作为新变量。

## 5、使用限制

导出，此工具目前只适用于Oracle。可以修改支持MySQL，我用不到，不做了


```
golang没有官方的Oracle 数据库连接驱动，而且现有的使用OCI的方式配置过于麻烦。 本工具只是个造数据的工具。。
采用的方式是执行sqlplus来执行Oracle SQL语句。
有ExecSQLPlus函数，执行SQL语句返回标准输出。

在主节点中有执行检测冲突的语句时，使用前后加特征字符串 ResultStart:'||count(*)||':ResultEnd 的方式，
然后截取中间的部分获得执行结果。

```


## 6、模板的产生
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
        SV6002000${dept.deptno0},${dept.dname},${dept.loc},
model/emp.unl        
        empno,ename,job,mgr,hiredate,sal,comm,deptno,
        1000123${emp.empno0},${emp.ename},MANAGER,1000133${emp.empno1},1981-06-09 00:00:00,2450,,SV6002000${dept.deptno0},
        1000133${emp.empno1},${emp.ename},PRESIDENT,,1981-11-17 00:00:00,5000,,SV6002000${dept.deptno0},
        1000143${emp.empno2},${emp.ename},CLERK,1000123${emp.empno0},1982-01-23 00:00:00,1300,,SV6002000${dept.deptno0},
```

主要的变化是deptno根据vardefine.json里配置的变量变为 SV6002000${deptno}。  
这里SV6002000是变量前缀，后面是表示需要替换变量，也即造数据时会替换成值数值。

所有的${var}在构造数据时统一替换为一个值，这个值默认是8位，最多支持造 1亿数据
可修改tools.go里的const Len = 9扩大取值范围。
这样的好处是多次导入时，容易控制取值段。

```
注意这里可能存在BUG，因为模板的产生过程中没有维护一个表字段的关联关系，
而是比如deptno是10，产生模板的时候会将其他表如emp表中值是10的变为SV6002000${deptno}
值长度越小，越容易出现这种问题，添加了一个WARN日志，长度小于4的，加了提示 WARN:Please check "variable" manually,maybe it's Wrong

维护表字段的关联关系太麻烦，而且记录数不一样，表很多的时候太难维护。比如我使用过程中50多张表需要构造，其中的关联关系很难维护。 通过这种方式可以自动寻找关联关系。
```


构造时将模板解析为：
```
    strslice: [ 'SV6002000' 'deptno' ',ACCOUNTING,NEW YORK,' ]
    repslice: [  0            1          0                   ]
```
构造时repslice中对应的值是0就直接复制原始字符串，是1就替换变量。



## 7、SQLLoader导入

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