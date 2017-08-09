set serveroutput on
Set feedback off
DECLARE  
  TYPE value_map_type IS TABLE OF varchar2(128) INDEX BY VARCHAR2(128);
  value_map value_map_type;

PROCEDURE export_table( QueryString in varchar2) IS
  c            NUMBER; --- corsor id
  col_cnt      INTEGER; --- column total
  rec_tab      DBMS_SQL.DESC_TAB;
  columnValue  varchar2(4000);
  status       integer;
  separator    varchar2(1);  --- 分隔符
  v_record_no  number ;
  filename     varchar2(64);
  v_variable   varchar2(128);
  TargetQueryString  varchar2(4000);
  v_tabl_sep   varchar2(64) := '--------------------';
BEGIN
  DBMS_OUTPUT.ENABLE (buffer_size=>null) ;
  filename := regexp_replace(regexp_substr(QueryString,'\*( )*from( )+([[:alnum:]_.])+'),'\*( )*from( )+([[:alnum:]_])+.','');   ---- 
  v_variable := substr(REGEXP_SUBSTR(QueryString,'\$[[:alpha:]_]+'),2);
  
  if v_variable is not null then
    TargetQueryString := replace(QueryString,'$'||v_variable, value_map(v_variable) );
    else TargetQueryString := QueryString;
  end if;

  --dbms_output.put_line( TargetQueryString );
  execute immediate 'alter session set nls_date_format=''YYYY-MM-DD HH24:MI:SS'' ';
  execute immediate 'alter session set nls_timestamp_format=''YYYY-MM-DD hh24:mi:ssSSS'' ';
  c := DBMS_SQL.OPEN_CURSOR;   ----- Open
  DBMS_SQL.PARSE(c, TargetQueryString, DBMS_SQL.NATIVE);  ---- Parse
  
  dbms_output.put_line( filename );

  DBMS_SQL.DESCRIBE_COLUMNS(c, col_cnt, rec_tab);
   separator := '';
  for i in 1 .. col_cnt loop
         dbms_sql.define_column( c, i, columnValue, 4000 ); ---- define column
         dbms_output.put(separator || lower(rec_tab(i).col_name) );-----在文件头输出列名
         separator := ',';
  end loop;
  dbms_output.put_line(separator);

  status := dbms_sql.execute(c);
  v_record_no := 0;
        while ( dbms_sql.fetch_rows(c) > 0 and v_record_no < 50  ) loop --- fetch rows  v_record_no <50,最多导出50 防止SQL有误，导出大量数据
           separator := '';
           for i in 1 .. col_cnt loop
               dbms_sql.column_value( c, i, columnValue );  ---- column value，第 i列的值，传给columnValue，而后输出columnValue
               if instr(columnValue,',') is not null or instr(columnValue,'"') is not null    ----包含需要转义的字符
                 then columnValue := replace(columnValue,'"','""');
                 columnValue := '"'||columnValue||'"';
               end if;
               dbms_output.put(separator || columnValue );
               separator := ',';
           end loop;
           dbms_output.put_line(separator); ---加一个分隔符，修复最后一个字段为空的情况
           v_record_no := v_record_no + 1;
       end loop;

  DBMS_SQL.CLOSE_CURSOR(c); ---- close
  
  dbms_output.put_line( v_tabl_sep );  ---输出表之间的分割线
END;

begin
    dbms_output.put_line('@@-%%-@@');

    export_table(' select * from scott.dept r where r.deptno = ''10'' ');
    export_table(' select * from scott.emp r where r.deptno = ''10'' ');
    export_table(' select * from scott.inf_subscriber ');

    dbms_output.put_line('@@-%%-@@');
end;
/