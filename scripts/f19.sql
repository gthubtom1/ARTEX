\pset pager off
select n.id,
       payload->>'vulnclass' as vc,
       left(coalesce(payload->>'name',''), 50) as nm,
       length(coalesce(payload->>'evidence','')) as evlen,
       left(coalesce(payload->>'evidence',''), 400) as ev
from exploration_nodes n
join tasks t on t.exploration_id = n.exploration_id
where t.id = 19 and n.kind = 'finding'
order by id;
