import json,glob,os,re,time,collections,datetime,sys
out=sys.argv[1]
cut=time.time()-30*86400
rx=re.compile(r'<command-name>/?([^<\s]+)</command-name>')
day=collections.defaultdict(lambda:{'sessions':0,'messages':0,'cache':0,'fires':0})
fires=collections.Counter();byday=collections.defaultdict(collections.Counter)
for f in glob.glob(os.path.expanduser('~/.claude/projects/**/*.jsonl'),recursive=True):
    if os.path.getmtime(f)<cut: continue
    seen=set()
    for l in open(f,errors='ignore'):
        if '"timestamp"' not in l: continue
        try:e=json.loads(l)
        except: continue
        ts=e.get('timestamp','')[:10]
        if not ts or e.get('type') not in('user','assistant'): continue
        if ts not in seen and '/subagents/' not in f:
            seen.add(ts);day[ts]['sessions']+=1
        m=e.get('message') or {}
        if not isinstance(m,dict):continue
        u=m.get('usage') or {}
        if e['type']=='assistant' and u:
            day[ts]['messages']+=1;day[ts]['cache']+=u.get('cache_read_input_tokens',0) or 0
        c=m.get('content')
        names=[]
        if e['type']=='assistant' and isinstance(c,list):
            for b in c:
                if isinstance(b,dict) and b.get('type')=='tool_use':
                    if b.get('name')=='Skill': names.append((b.get('input') or {}).get('skill',''))
                    elif str(b.get('name','')).startswith('mcp__'): names.append(b['name'].split('__')[1])
        elif e['type']=='user' and not e.get('isMeta'):
            t=c if isinstance(c,str) else ''.join(x.get('text','') for x in c if isinstance(x,dict) and x.get('type')=='text') if isinstance(c,list) else ''
            names+=rx.findall(t)
        for n in names:
            if n: fires[n]+=1;byday[n][ts]+=1;day[ts]['fires']+=1
today=datetime.date.today()
dates=[(today-datetime.timedelta(days=i)).isoformat() for i in range(29,-1,-1)]
json.dump({'dates':dates,'days':[day[d] for d in dates],'fires':fires.most_common(20),'byday':{k:[v.get(d,0) for d in dates] for k,v in byday.items()}},open(out,'w'))
d=json.load(open(out));print('sessions/day',[x['sessions'] for x in d['days']]);print(d['fires'][:10]);print('cache total %.2gB'%(sum(x['cache'] for x in d['days'])/1e9))
