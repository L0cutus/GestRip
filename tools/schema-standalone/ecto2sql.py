# Traduce le migrazioni Ecto dello standalone (il sottoinsieme del DSL che usano) in SQL SQLite e
# le applica in ordine a un database vuoto: il risultato è lo schema finale a go-baseline.
# Le 11 migrazioni con funzioni private o interpolazioni sono scritte a mano in overrides/
# (una per versione, il nome del file è la versione). Scritto dal revisore della slice 018.
# Uso: python3 -I ecto2sql.py <cartella delle .exs> <database da creare>
# Lo lancia rigenera.sh; vedi README.md.
import re, sys, os, sqlite3, json
MIG = sys.argv[1]; OUT = sys.argv[2]

TYPES = {'string':'TEXT','text':'TEXT','integer':'INTEGER','bigint':'INTEGER','boolean':'INTEGER',
 'utc_datetime_usec':'TEXT','utc_datetime':'TEXT','date':'TEXT','map':'TEXT','binary':'BLOB'}
ONDEL = {'restrict':' ON DELETE RESTRICT','delete_all':' ON DELETE CASCADE','nilify_all':' ON DELETE SET NULL','nothing':''}

def strip_comments(s):
    out=[]
    for line in s.split('\n'):
        if re.match(r'^\s*#(?!\{)', line): continue
        out.append(line)
    return '\n'.join(out)

def body_of(src, name):
    # trova "def name do" ... matching end (by indentation: 2 spaces)
    m = re.search(r'^  def %s(\(\))? do\n' % name, src, re.M)
    if m:
        rest = src[m.end():]
        e = re.search(r'^  end\n', rest, re.M)
        return rest[:e.start()]
    m = re.search(r'^  def %s, do: (.*)$' % name, src, re.M)
    if m: return m.group(1)
    return None

def split_top(s):
    # split args by commas at depth 0
    parts=[];d=0;cur='';i=0;instr=None
    while i<len(s):
        c=s[i]
        if instr:
            cur+=c
            if c=='\\': cur+=s[i+1]; i+=2; continue
            if c==instr: instr=None
            i+=1; continue
        if c=='"': instr='"'; cur+=c
        elif c in '([{': d+=1; cur+=c
        elif c in ')]}': d-=1; cur+=c
        elif c==',' and d==0: parts.append(cur.strip()); cur=''
        else: cur+=c
        i+=1
    if cur.strip(): parts.append(cur.strip())
    return parts

def estr(x):
    x=x.strip()
    # concatenation with <>
    if '<>' in x:
        return ''.join(estr(p) for p in re.split(r'\s*<>\s*', x))
    m=re.match(r'^"(.*)"$', x, re.S)
    return m.group(1).replace('\\"','"')

def opts(parts):
    o={}
    for p in parts:
        m=re.match(r'^(\w+):\s*(.*)$', p, re.S)
        if m: o[m.group(1)]=m.group(2).strip()
    return o

def dflt(v):
    if v=='true': return '1'
    if v=='false': return '0'
    if v=='%{}': return "'{}'"
    if v.startswith('"'): return "'"+estr(v)+"'"
    return v

def coldef(table, args):
    name=args[0].lstrip(':'); typ=args[1]; o=opts(args[2:])
    fk=''
    m=re.match(r'references\((.*)\)$', typ, re.S)
    if m:
        ra=split_top(m.group(1)); ro=opts(ra[1:])
        fk=' CONSTRAINT "%s_%s_fkey" REFERENCES "%s"("id")%s' % (table,name,ra[0].lstrip(':'),ONDEL[ro.get('on_delete',':nothing').lstrip(':')])
        sqlt='INTEGER'
    else:
        sqlt=TYPES[typ.lstrip(':')]
    s='"%s" %s' % (name, sqlt)
    pk = o.get('primary_key')=='true'
    if 'default' in o: s+=' DEFAULT '+dflt(o['default'])
    if o.get('null')=='false': s+=' NOT NULL'
    s+=fk
    if 'check' in o:
        co=opts(split_top(o['check'].strip()[2:-1]))
        s+=' CONSTRAINT "%s" CHECK (%s)' % (estr(co['name']), estr(co['expr']))
    return name, s, pk

def find_calls(s):
    """yield statements at top level of a block: returns list of (kind, text)"""
    i=0; res=[]
    n=len(s)
    while i<n:
        m=re.compile(r'(create table|alter table|create\(|create |execute\(|execute |flush\(\)|add\(|add |timestamps|remove|drop)').search(s,i)
        if not m: break
        res.append(m); i=m.end()
    return res

def grab_paren(s, i):
    # s[i]=='(' ; return content and end
    d=0;j=i;instr=None
    while j<len(s):
        c=s[j]
        if instr:
            if s.startswith('"""',j) and instr=='"""': instr=None; j+=3; continue
            if instr=='"' and c=='\\': j+=2; continue
            if instr=='"' and c=='"': instr=None
            j+=1; continue
        if s.startswith('"""',j): instr='"""'; j+=3; continue
        if c=='"': instr='"'
        elif c=='(': d+=1
        elif c==')':
            d-=1
            if d==0: return s[i+1:j], j+1
        j+=1
    raise Exception('unbalanced')

def heredoc_or_str(a):
    a=a.strip()
    if a.startswith('"""'):
        t=a[3:a.rindex('"""')]
        t=t.lstrip('\n')
        return t
    if a.startswith('~s|'): return a[3:a.rindex('|')]
    return estr(a)

def translate(body, sql):
    lines=body.split('\n'); i=0
    while i<len(lines):
        L=lines[i].strip()
        if not L: i+=1; continue
        m=re.match(r'^(create|alter) table\((.*)\) do$', L)
        if m:
            ta=split_top(m.group(2)); table=ta[0].lstrip(':'); to=opts(ta[1:])
            blk=[]; i+=1
            while lines[i].strip()!='end': blk.append(lines[i]); i+=1
            i+=1
            text='\n'.join(blk)
            # gather calls
            calls=[]; j=0
            while True:
                mm=re.compile(r'\b(add|timestamps|remove)\b\s*(\(?)').search(text,j)
                if not mm: break
                if mm.group(2)=='(':
                    c,e=grab_paren(text, mm.end()-1); j=e
                else:
                    e=text.find('\n',mm.end()); e=len(text) if e<0 else e
                    c=text[mm.end():e]; j=e
                calls.append((mm.group(1), split_top(c)))
            if m.group(1)=='create':
                cols=[]; pks=[]
                if to.get('primary_key')!='false':
                    cols.append('"id" INTEGER PRIMARY KEY AUTOINCREMENT')
                for k,a in calls:
                    if k=='add':
                        n,d,pk=coldef(table,a)
                        if pk: pks.append(n)
                        cols.append(d)
                    elif k=='timestamps':
                        o=opts(a)
                        if o.get('inserted_at')!='false': cols.append('"inserted_at" TEXT NOT NULL')
                        if o.get('updated_at')!='false': cols.append('"updated_at" TEXT NOT NULL')
                if pks: cols.append('PRIMARY KEY (%s)' % ','.join('"%s"'%p for p in pks))
                sql.append('CREATE TABLE "%s" (%s)' % (table, ', '.join(cols)))
            else:
                for k,a in calls:
                    if k=='add':
                        n,d,pk=coldef(table,a); sql.append('ALTER TABLE "%s" ADD COLUMN %s' % (table,d))
                    elif k=='remove':
                        sql.append('ALTER TABLE "%s" DROP COLUMN "%s"' % (table,a[0].lstrip(':')))
                    elif k=='timestamps':
                        raise Exception('ts in alter')
            continue
        if L=='create(':
            lines[i+1]='create('+lines[i+1].strip(); i+=1; L=lines[i].strip()
        m=re.match(r'^create\(?\s*(unique_index|index)\(', L)
        if m:
            # gather until balanced
            txt=L; 
            while txt.count('(')>txt.count(')'): i+=1; txt+=' '+lines[i].strip()
            i+=1
            k=txt.index(m.group(1)+'(')+len(m.group(1))
            c,_=grab_paren(txt,k)
            a=split_top(c); table=a[0].lstrip(':')
            cols=re.findall(r':(\w+)', a[1]); o=opts(a[2:])
            name=o.get('name', ':%s_%s_index'%(table,'_'.join(cols))).lstrip(':')
            where=' WHERE '+estr(o['where']) if 'where' in o else ''
            sql.append('CREATE %sINDEX "%s" ON "%s" (%s)%s' % ('UNIQUE ' if m.group(1)=='unique_index' else '', name, table, ', '.join('"%s"'%x for x in cols), where))
            continue
        m=re.match(r'^execute[\( ]', L)
        if m:
            txt=lines[i]; 
            # collect until parens balanced (heredoc-aware)
            j=i
            full='\n'.join(lines[i:])
            st=full.index('execute')+7
            if full[st]=='(':
                c,e=grab_paren(full,st)
                consumed=full[:e].count('\n')
            else:
                # execute "...", no parens: single line
                c=full[st:full.index('\n',st)] if '\n' in full[st:] else full[st:]
                consumed=0
            a=split_top(c)
            sql.append(heredoc_or_str(a[0]))
            i+=consumed+1
            continue
        if L.startswith('flush') : i+=1; continue
        raise Exception('non gestito: %r' % L)

overrides = {}
od=os.path.join(os.path.dirname(os.path.abspath(__file__)),'overrides')
for f in os.listdir(od): overrides[f.replace('.sql','')]=open(os.path.join(od,f)).read()

con=sqlite3.connect(OUT); con.execute('PRAGMA foreign_keys=ON')
log=open(OUT+'.sql','w')
for f in sorted(os.listdir(MIG)):
    ver=f.split('_')[0]
    if ver in overrides:
        stmts=[overrides[ver]]
        con.executescript(overrides[ver]); log.write('-- '+f+' (override)\n'+overrides[ver]+'\n'); continue
    src=strip_comments(open(os.path.join(MIG,f)).read())
    body=body_of(src,'up') or body_of(src,'change')
    sql=[]
    translate(body, sql)
    log.write('-- '+f+'\n')
    for s in sql:
        log.write(s+';\n')
        try: con.executescript(s if s.rstrip().endswith(';') else s+';')
        except Exception as e: print('ERR', f, e, s[:200]); raise
con.commit()
print('ok')
