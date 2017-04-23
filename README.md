# dataTool
一个使用Go语言编写的，用于生成大量性能测试数据的工具

采取的方案是使用系统产生少量数据。
以这少量用户数据为模板，利用模板大批量构造数据文件，
使用SqlLoader导入Oracle数据库。 MySQL数据库也有类似的 LOAD 可以导入数据。



实际使用中造文件的速度取决于磁盘的性能。 本人使用过程中，服务器上磁盘性能较好，
而且配置了分别输出到两个磁盘上，总的速度达到了 500M/s以上。

实际上最耗时的步骤是在导入数据到数据库时，MySQL没有使用过，但是Oracle的SqlLoader
的速度真的不怎么快。

1、配置

2、使用

3、详解
![image](https://github.com/ANBUZHIDAO/dataTool/blob/master/picture/dataTool%E6%B5%81%E7%A8%8B%E5%9B%BE%E8%A7%A3.JPG)

初始化几个Bufferstruct，在管道和线程之间组成一个循环圈，Bufferstruct中的buf是byte切片，分配足够的内存，且过程中检查长度来中止构造，
避免运行过程中内存分配，这种方式基本是构造字符串十分高效，比string join和+高效得多。

使用了Go语言的goroutine，
bufferToFile 负责将构造好的数据写入文件。
buildBytes 负责构造字符串。

全部完成后通知主程序。

此goroutine协程只负责构造字符串，byte切片buf中不断写入，剩余长度小于30000时，将Bufferstruct写入。

buildBytes协程可根据配置启动多个，bufferToFile只有1个。
buildBytes构造完后将Bufferstruct.endFlag置为true，bufferToFile根据接收到的endFlag数量判断是否结束，如果已全部结束，写入一个消息到complete管道，来通知main主程序。