set serveroutput on
Set feedback off

declare
      v_sql varchar2(4000);
      v_logflag varchar2(3);
      v_indexname varchar2(256);
cursor reIndex is
  select 'alter index '||a.owner||'.'||a.index_name ||' rebuild nologging parallel 32' , a.logging, a.owner||'.'||a.index_name
from dba_indexes a where a.status = 'UNUSABLE'
union all select 'alter index '||b.index_owner||'.'||b.index_name ||' rebuild partition '|| PARTITION_NAME || ' parallel 32', b.logging,b.index_owner||'.'||b.index_name  
from dba_ind_partitions b where b.status = 'UNUSABLE'
union all
select 'alter index '||t.index_owner||'.'||t.index_name||' rebuild subpartition '||t.subpartition_name || ' parallel 32', t.logging ,t.index_owner||'.'||t.index_name 
from dba_ind_subpartitions t where t.status='UNUSABLE';

BEGIN
  dbms_output.enable (buffer_size=>null) ;
  open reIndex;
  loop
    
  fetch reIndex into v_sql,v_logflag,v_indexname ;
     exit when reIndex%notfound;
   
  dbms_output.put_line(v_sql); 
  execute immediate(v_sql);
  execute immediate('alter index '||v_indexname||' noparallel');
  
  if(v_logflag = 'YES') then
    execute immediate('alter index '||v_indexname||' logging');
  end if;

  end loop; 
  close reIndex;
  
  --dbms_stats.gather_schema_stats(ownname => 'username',estimate_percent => 10,degree =>16,cascade => true,granularity => 'auto',force=> true);

  ---execute immediate('alter system flush shared_pool');

END;
/