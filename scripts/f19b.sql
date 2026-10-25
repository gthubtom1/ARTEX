\pset pager off
\t on
select payload->>'evidence' from exploration_nodes n join tasks t on t.exploration_id=n.exploration_id where t.id=19 and n.kind='finding';
