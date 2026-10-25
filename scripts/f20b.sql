\pset pager off
\t on
select n.id, coalesce(n.payload->>'evidence','') from exploration_nodes n join tasks t on t.exploration_id=n.exploration_id where t.id=20 and n.kind='finding' order by n.id;
