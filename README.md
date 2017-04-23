               dataTool
===
# dataTool简介
    一个使用Go语言编写的，用于生成大量性能测试数据的工具，工具的目的快速，方便地构造大批量数据。

    采取的方案是使用系统产生少量数据。
以这少量用户数据为模板，利用模板大批量构造CSV格式的数据文件，
使用SqlLoader导入Oracle数据库。 MySQL数据库也有类似的 LOAD 可以导入数据。


    实际使用中造文件的速度取决于磁盘的性能。 本人使用过程中，服务器上磁盘性能较好，
而且配置了分别输出到两个磁盘上，总的速度达到了 500M/s以上。
    实际上最耗时的步骤是在导入数据到数据库时，MySQL我没有使用过，但是Oracle的
SqlLoader的速度真的不怎么快，1000多万数据，几十张表，共500多G的数据，构造只需要不到20分钟，
导入Oracle数据库并重建索引，做完表分析需要几个小时。


##1、配置

##2、使用

将数据库表导出为CSV格式
go run genModel.go
go run hello.go -s 1000 -t 5


##3、模板的产生

deptno,dname,loc,
10,ACCOUNTING,NEW YORK,

生成的模板：
deptno,dname,loc,
SV6002000${deptno},ACCOUNTING,NEW YORK,

主要的变化是deptno根据配置的变量变为 SV6002000${deptno}。  
这里前面是变量前缀，后面才是根据入参不断变化的值，也即所有的变量值得后8位时同步变化的。
默认是8位，可修改一个值更改。
这样的好处是多次导入时，容易控制取值段。

另外emp表里的deptno字段也同步变化的，变为 SV6002000${deptno}


构造时将模板解析为：
strslice: [ 'SV6002000' 'deptno' ',ACCOUNTING,NEW YORK,' ]
repslice: [  0            1          0                   ]

构造时repslice中对应的值是0就直接复制原始字符串，是1就替换变量。



##4、文件构造详解

![image](https://github.com/ANBUZHIDAO/dataTool/blob/master/picture/dataTool%E6%B5%81%E7%A8%8B%E5%9B%BE%E8%A7%A3.JPG)

初始化几个Bufferstruct，在管道和线程之间组成一个循环圈，Bufferstruct中的buf是byte切片，分配足够的内存，且过程中检查长度来中止构造，
避免运行过程中内存分配，这种方式基本是构造字符串十分高效，比string join和+高效得多。

使用了Go语言的goroutine，
bufferToFile 负责将构造好的数据写入文件。
buildBytes 负责构造字符串。

全部完成后通知主程序。

此goroutine协程只负责构造字符串，byte切片buf中不断增长，剩余长度小于30000时，将Bufferstruct写入。

buildBytes协程可根据配置启动多个，bufferToFile只有1个。
buildBytes构造完后将Bufferstruct.endFlag置为true，bufferToFile根据接收到的endFlag数量判断是否结束，如果已全部结束，写入一个消息到complete管道，来通知main主程序。

##5、SQLLoader导入

SqlLoader导入有2种方式，传统路径导入、直接路径导入。两种方式具体的可以自己找资料。

http://docs.oracle.com/database/122/nav/portal_booklist.htm 
Oracle官方文档列表，其中的 Utilities 详细讲述了 SqlLoader。

目前默认的直接路径导入的控制文件格式是
OPTIONS(DIRECT=Y,SKIP_INDEX_MAINTENANCE=Y)  
---- DIRECT=Y 表示是直接路径导入，SKIP_INDEX_MAINTENANCE=Y 不维护索引，导入后索引失效，然后统一重建索引。
UNRECOVERABLE    ---- 不产生 redo log和archive log，以提高导入速度。如果导入后没有备份，数据文件就损坏了，无法恢复。
LOAD DATA 
INFILE '${infile}'
APPEND
into table ${username}.${tablename}
fields TERMINATED BY "," optionally enclosed by '"'    ---是标准的CSV格式的文件，可能含有需要转义的字符串
(${header})


默认如果构造的数量小于50万就使用传统路径，50万以上使用直接路径。


导入后，执行 RebuildAndGather.sql 查找出失效的索引，统一重建索引，重建索引时使用并行。
重建完索引后做表分析。
做完表分析之后，执行alter system flush shared_pool 清空共享池。
