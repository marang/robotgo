"""Run native Dagu polling regressions; requires Python, PyYAML and DAGU_BIN."""
from pathlib import Path
import copy,json,subprocess,tempfile,yaml,threading,os,shutil,re
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
from http.client import HTTPConnection

repo=Path(__file__).resolve().parent.parent
home=Path(tempfile.mkdtemp(prefix='robotgo-watch-runtime-'))
config=home/'config.yaml';config.write_text('skip_examples: true\nauth:\n  mode: none\n')
binary=os.environ.get('DAGU_BIN') or shutil.which('dagu')
if not binary:raise SystemExit('Set DAGU_BIN to the Dagu CLI used by the target server')
print('Native Dagu fixture evidence:',home,flush=True)
# Validate installed filenames: Dagu infers the DAG name from the filename.
# Validating only short temporary names misses the real nested-child name gate.
for template, name in {'pipeline': 'ci', 'watch': 'watch', 'security': 'security', 'dependencies': 'dependencies'}.items():
    path = home / ('robotgo-ovh-' + name + '.yaml')
    path.write_bytes((repo / '.dagu' / (template + '.yaml')).read_bytes())
    checked = subprocess.run([binary, 'validate', str(path), '--dagu-home', str(home),
                              '--config', str(config)], capture_output=True, text=True)
    assert checked.returncode == 0, (path.name, checked.stdout + checked.stderr)
ci=yaml.safe_load((repo/'.dagu/pipeline.yaml').read_text())['steps'][0]
watch=yaml.safe_load((repo/'.dagu/watch.yaml').read_text())
planner=next(s for s in watch['steps'] if s['id']=='plan')
guard=next(s for s in watch['steps'] if s['id']=='ci')['foreach']['steps'][-1]
results=[]

def run(name,graph,success=True,run_id=None):
    path=home/(name+'.yaml');path.write_text(yaml.safe_dump(graph,sort_keys=False))
    r=subprocess.run([binary,'start',str(path),'--dagu-home',str(home),'--config',str(config)]+(['--run-id',run_id] if run_id is not None else []),capture_output=True,text=True)
    (home/(name+'.log')).write_text(r.stdout+r.stderr)
    if success is not None: assert (r.returncode==0)==success,(name,r.returncode,(r.stdout+r.stderr)[-3000:])
    results.append({'name':name,'exit':r.returncode,'expected':success});print(json.dumps(results[-1]),flush=True)
    return r

def js_case(name,step,inputs,expected=None,success=True):
    step=copy.deepcopy(step);step.pop('depends',None);step['with']['input']=inputs
    steps=[step]
    if expected is not None:
        expr='${'+step['id']+'.output.source}' if step.get('output') else None
        if step['id']=='plan':expr='${plan.output.changes}'
        if expr:
            steps.append({'id':'assert','depends':[step['id']],'action':'js.run','with':{'input':expr,'script':expected}})
    run(name,{'type':'graph','steps':steps},success)

noop={'type':'graph','steps':[{'id':'noop','action':'js.run','with':{'script':'return true;'}}]}
run('native-id-64',noop,run_id='x'*64)
rejected=run('native-id-65',noop,success=False,run_id='x'*65)
assert 'dag-run ID length' in rejected.stdout+rejected.stderr
rejected=run('native-id-dotted',noop,success=False,run_id='release.v0.10.1')
assert 'alphanumeric' in rejected.stdout+rejected.stderr

A='a'*40;B='b'*40;C='c'*40;D='d'*40;E='e'*40
payload={'repository':'et/marang/robotgo','commit':B,'ref':'refs/heads/feature/test','tag':''}
js_case('ci-manual',ci,{'requested':A,'source_ref':'refs/heads/feature/test','payload':''},'if(input.commit!=="'+A+'"||input.ref!=="refs/heads/feature/test")throw new Error("wrong source");')
js_case('ci-manual-main',ci,{'requested':'main','payload':''},'if(input.ref!=="refs/heads/main")throw new Error("wrong main ref");')
js_case('ci-manual-missing-ref',ci,{'requested':A,'payload':''},success=False)
js_case('ci-manual-invalid-ref',ci,{'requested':A,'source_ref':'refs/tags/v1.0.4','payload':''},success=False)
js_case('ci-ref-conflict',ci,{'requested':'main','source_ref':'refs/heads/other','payload':json.dumps(payload)},success=False)
js_case('ci-webhook',ci,{'requested':'main','payload':json.dumps(payload)},'if(input.commit!=="'+B+'"||input.ref!=="refs/heads/feature/test")throw new Error("wrong source");')
js_case('ci-conflict',ci,{'requested':A,'payload':json.dumps(payload)},success=False)
js_case('ci-wrong-repo',ci,{'requested':'main','payload':json.dumps({**payload,'repository':'other'})},success=False)
js_case('ci-json-null',ci,{'requested':'main','payload':'null'},success=False)
js_case('ci-internal-ref',ci,{'requested':'main','payload':json.dumps({**payload,'ref':'refs/heads/entire/checkpoints/v1'})},success=False)
js_case('ci-bad-sha',ci,{'requested':'main','payload':json.dumps({**payload,'commit':'main'})},success=False)
base=f'{A}\trefs/heads/main\n{B}\trefs/tags/v0.1.0\n{C}\trefs/tags/v0.1.0^{{}}\n{D}\trefs/heads/release-artifacts/v0.1.0\n'
old={'refs/heads/main':A}
previous={'found':True,'value':old,'version':1}
js_case('plan-baseline',planner,{'refs':base,'previous':{'found':False,'value':None}},'if(!input.baseline||input.ci.length||Object.keys(input.snapshot).length!==1)throw new Error("bad baseline");')
js_case('plan-unchanged',planner,{'refs':base,'previous':previous},'if(input.ci.length)throw new Error("duplicate");')
changed=base+f'{D}\trefs/heads/feature/one\n{D}\trefs/heads/feature/two\n{D}\trefs/tags/v0.1.1\n{E}\trefs/tags/v0.1.1^{{}}\n'
js_case('plan-dedupe-and-ignore-tags',planner,{'refs':changed,'previous':previous},'if(input.ci.length!==1||input.ci[0].id!=="robotgo-ci-'+D+'"||JSON.parse(input.ci[0].body).payload.commit!=="'+D+'")throw new Error("bad plan");')
js_case('ci-tag-rejected',ci,{'requested':'main','payload':json.dumps({**payload,'ref':'refs/tags/v1.0.0'})},success=False)
js_case('plan-missing-main',planner,{'refs':f'{B}\trefs/heads/feature/one\n','previous':previous},success=False)
js_case('plan-invalid-snapshot',planner,{'refs':base,'previous':{'found':True,'value':[]}},success=False)
js_case('plan-string-snapshot',planner,{'refs':base,'previous':json.dumps({'found':True,'value':json.dumps(old)})},'if(input.ci.length)throw new Error("duplicate");')
js_case('plan-malformed-ref',planner,{'refs':base+'bad response\n','previous':previous},success=False)
for name,response,success in [('200',{'status_code':200},True),('duplicate',{'status_code':409,'body':{'code':'already_exists','message':'dag-run with ID example already exists'}},True),('wrong409',{'status_code':409,'body':{}},False),('500',{'status_code':500},False),('network',{'status_code':0},False)]:
    js_case('guard-'+name,guard,{'response':json.dumps(response),'id':'example'},success=success)

class Handler(BaseHTTPRequestHandler):
    seen=set();calls=[];failure_id=None;drop_id=None
    def log_message(self,*args): pass
    def do_POST(self):
        body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));runid=body['dagRunId'];self.calls.append(runid)
        if self.drop_id==runid:
            self.connection.close();return
        if self.headers.get('Authorization')!='Bearer fixture':code=401;data={}
        elif not isinstance(runid,str) or not re.fullmatch(r'[A-Za-z0-9_-]+',runid) or len(runid)>64:code=400;data={'code':'invalid_argument','message':'invalid dag-run ID'}
        elif self.failure_id==runid:code=500;data={}
        elif runid in self.seen:code=409;data={'code':'already_exists','message':f'dag-run with ID {runid} already exists'}
        else:self.seen.add(runid);code=200;data={'dagRunId':runid}
        raw=json.dumps(data).encode();self.send_response(code);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(raw)
server=ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
fixture=copy.deepcopy(watch)
for key in ['worker_selector','secrets','schedule','queue']:fixture.pop(key,None)
for group in ['ci']:
    node=next(s for s in fixture['steps'] if s['id']==group)
    http=next(s for s in node['foreach']['steps'] if s['id']=='enqueue');http['with']['url']=f'http://127.0.0.1:{server.server_port}/{group}';http['with']['headers']['Authorization']='Bearer fixture'
def full(refs,success=True):
    fixture['steps'][0]={'id':'refs','action':'js.run','with':{'script':'return '+json.dumps(refs)+';'},'output':'REFS'}
    return run('full-watch',fixture,success)
try:
    for runid,expected in [('release.v0.10.1',400),('robotgo-ci-'+E,200),('x'*65,400),('x'*64,200)]:
        connection=HTTPConnection('127.0.0.1',server.server_port,timeout=5)
        try:
            connection.request('POST','/contract',json.dumps({'dagRunId':runid}),{'Authorization':'Bearer fixture','Content-Type':'application/json'})
            response=connection.getresponse();assert response.status==expected,(runid,response.status);response.read()
        finally:connection.close()
    Handler.calls.clear();Handler.seen.clear()
    full(base);assert not Handler.calls
    full(changed);assert len(Handler.seen)==1
    before=len(Handler.calls);full(changed);assert len(Handler.calls)==before
    newer=changed+f'{B}\trefs/heads/feature/new-one\n{C}\trefs/heads/feature/new-two\n'
    Handler.failure_id='robotgo-ci-'+C
    full(newer,False);count=len(Handler.calls)
    Handler.failure_id=None
    full(newer);assert len(Handler.calls)>count and Handler.calls.count('robotgo-ci-'+B)>=2
    count=len(Handler.calls);full(newer);assert len(Handler.calls)==count
    network_refs=newer+f'{A}\trefs/heads/feature/network-one\n{E}\trefs/heads/feature/network-two\n'
    Handler.drop_id='robotgo-ci-'+E
    full(network_refs,False)
    Handler.drop_id=None
    full(network_refs)
    count=len(Handler.calls);full(network_refs);assert len(Handler.calls)==count
finally:server.shutdown();server.server_close()
# The worker base uses shell:direct. Exit-handler command preconditions need
# an explicit DAG shell, including when steps use only built-in actions.
handler=copy.deepcopy(yaml.safe_load((repo/'.dagu/pipeline.yaml').read_text())['handler_on']['exit'])
for name,present,legacy in [('artifacts-present',True,False),('artifacts-absent',False,False),('artifacts-shell-control',True,True)]:
    destination=home/(name+'-artifacts')
    current=copy.deepcopy(handler)
    current['with']['destination']=str(destination)
    graph={'type':'graph','shell':'direct' if legacy else yaml.safe_load((repo/'.dagu/pipeline.yaml').read_text())['shell'],'artifacts':{'enabled':True},'handler_on':{'exit':current},'steps':[]}
    if present:
        graph['steps']=[{'id':'report','action':'file.write','with':{'path':'${context.paths.work_dir}/sway-evidence/attempt/native-input/test.log','content':'sanitized fixture\n','create_dirs':True}}]
    else:
        graph['steps']=[{'id':'noop','action':'js.run','with':{'script':'return true;'}}]
    result=run(name,graph)
    retained=destination/'attempt/native-input/test.log'
    if present and not legacy:
        assert retained.read_text()=='sanitized fixture\n'
    else:
        assert not destination.exists(),name
    if legacy:
        records=list((home/'data/dag-runs'/name).rglob('status.jsonl'))
        assert len(records)==1
        status=json.loads(records[0].read_text().splitlines()[-1])
        assert status['onExit']['status']==5
        error=status['onExit']['step']['preconditions'][0]['error']
        assert 'direct' in error and 'executable file not found' in error

(home/'results.json').write_text(json.dumps(results,indent=2)+'\n')
print('ALL RUNTIME CHECKS PASSED',len(results),'evidence',home)
