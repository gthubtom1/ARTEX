\pset pager off
select 'GOAL' as t, n.id, n.state, left(coalesce(n.payload->>'text', n.payload->>'name',''),110) as txt
from exploration_nodes n join tasks t on t.exploration_id=n.exploration_id
where t.id=20 and n.kind='goal' order by n.id;
select 'FINDING' as t, n.id, left(coalesce(n.payload->>'name',''),110) as nm,
       length(coalesce(n.payload->>'evidence','')) as evlen
from exploration_nodes n join tasks t on t.exploration_id=n.exploration_id
where t.id=20 and n.kind='finding' order by n.id;
