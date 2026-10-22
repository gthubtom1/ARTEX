import re, json, os

src = open('kb/capabilities/taxonomy.md', encoding='utf-8').read()
rows = []
group = 0
for line in src.splitlines():
    m = re.match(r'^##\s*组\s*(\d+)', line)
    if m:
        group = int(m.group(1)); continue
    if not line.startswith('|'): continue
    cells = [c.strip() for c in line.strip().strip('|').split('|')]
    if len(cells) < 3: continue
    if not re.match(r'^\d+\.\d+$', cells[0]): continue
    item = cells[0]
    can = cells[1]; plain = cells[2]
    rest = cells[3:]
    proof = value = stage = ''
    if len(rest) == 3:
        proof, value, stage = rest
    elif len(rest) == 2:
        if rest[0].startswith(('★', '—')): value, stage = rest
        else: proof, value = rest
    elif len(rest) == 1:
        value = rest[0]
    elif not rest:
        plain, value, stage = cells[2], '—', '禁止'
    rows.append(dict(group=group, item=item, can=can, plain=plain, proof=proof, value=value, stage=stage))

os.makedirs('internal/capability', exist_ok=True)
json.dump(rows, open('internal/capability/taxonomy_source.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=1)

out = ['// Code generated from kb/capabilities/taxonomy.md by hand-run script; DO NOT EDIT.', '', 'package capability', '',
       '// taxonomyData 是 13 组能力清单（解析自 kb/capabilities/taxonomy.md）。', 'var taxonomyData = []Taxon{']
for r in rows:
    out.append('\t{Group: %d, Item: %s, Can: %s, Plain: %s, Proof: %s, Value: %s, Stage: %s},' % (
        r['group'], json.dumps(r['item'], ensure_ascii=False), json.dumps(r['can'], ensure_ascii=False),
        json.dumps(r['plain'], ensure_ascii=False), json.dumps(r['proof'], ensure_ascii=False),
        json.dumps(r['value'], ensure_ascii=False), json.dumps(r['stage'], ensure_ascii=False)))
out.append('}')
open('internal/capability/taxonomy_gen.go', 'w', encoding='utf-8').write('\n'.join(out) + '\n')
print('rows', len(rows), 'groups', sorted({r['group'] for r in rows}))
