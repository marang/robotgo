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
pipeline=yaml.safe_load((repo/'.dagu/pipeline.yaml').read_text())
ci=pipeline['steps'][0]
assert pipeline['overlap_policy']=='all' and pipeline['retry_policy']['limit']==0
assert [step['id'] for step in pipeline['steps'][:5]]==[
    'normalize','checkout','verify_source','capture_source_evidence','github_mirror']
assert pipeline['steps'][4]['depends']==['capture_source_evidence']
pipeline_steps = {step['id']: step for step in pipeline['steps']}
assert pipeline_steps['verify_github_mirror']['depends'] == ['github_mirror']
assert pipeline_steps['capture_github_mirror_evidence']['depends'] == ['verify_github_mirror']
assert pipeline_steps['prepare']['depends'] == ['capture_github_mirror_evidence']
watch=yaml.safe_load((repo/'.dagu/watch.yaml').read_text())
planner=next(s for s in watch['steps'] if s['id']=='plan')
guard=next(s for s in watch['steps'] if s['id']=='ci')['foreach']['steps'][-1]
results=[]

def run(name,graph,success=True,run_id=None,params=()):
    path=home/(name+'.yaml');path.write_text(yaml.safe_dump(graph,sort_keys=False))
    command=[binary,'start',str(path),'--dagu-home',str(home),'--config',str(config)]
    if run_id is not None: command.extend(['--run-id',run_id])
    if params: command.extend(['--',*params])
    r=subprocess.run(command,capture_output=True,text=True)
    (home/(name+'.log')).write_text(r.stdout+r.stderr)
    if success is not None: assert (r.returncode==0)==success,(name,r.returncode,(r.stdout+r.stderr)[-3000:])
    results.append({'name':name,'exit':r.returncode,'expected':success});print(json.dumps(results[-1]),flush=True)
    return r

def js_case(name,step,inputs,expected=None,success=True):
    step=copy.deepcopy(step);step.pop('depends',None);step['with']['input']=inputs
    if step['id']=='normalize':
        step['with']['input']={'ref':'','source_ref':'',**inputs}
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

plugin_ref=payload['ref']
plugin_input={'requested':A,'ref':plugin_ref,'payload':''}
expected=f'if(input.commit!=="{A}"||input.ref!=="{plugin_ref}")throw new Error("wrong plugin source");'
js_case('ci-plugin',ci,plugin_input,expected)
js_case('ci-plugin-matching-alias',ci,{**plugin_input,'source_ref':plugin_ref},expected)
js_case('ci-plugin-conflicting-alias',ci,{**plugin_input,'source_ref':'refs/heads/other'},success=False)
js_case('ci-plugin-conflicting-event',ci,{**plugin_input,'requested':B,'ref':'refs/heads/other','payload':json.dumps(payload)},success=False)
js_case('ci-plugin-invalid-ref',ci,{**plugin_input,'ref':'refs/heads/invalid//ref'},success=False)
js_case('ci-zero-sha',ci,{**plugin_input,'requested':'0'*40},success=False)
js_case('ci-manual-main-mismatched-ref',ci,{'requested':'main','source_ref':plugin_ref,'payload':''},success=False)

def source_fixture(name, *, mode=None, checkout_sha=A, mirror=None, success=True,
                   expect_source=True, expect_mirror=False):
    # Keep the graph, gates and artifact destinations. Stub external execution.
    if mirror is None:
        mirror = {'source': A, 'tree': C, 'branch': 'ci/entire/' + A, 'already_mirrored': False}
    actions = {
        'checkout': 'return ' + json.dumps({'commit': checkout_sha}),
        'github_mirror': 'return ' + json.dumps(mirror),
        'github_native': 'return {status: "fixture"}',
    }
    marker_dir = home / (name + '.containers')
    steps = copy.deepcopy(pipeline['steps'])
    containers = set()
    for step in steps:
        if step.get('action') == 'container.run':
            containers.add(step['id'])
            step['action'] = 'file.write'
            step['with'] = {'path': str(marker_dir / step['id']), 'content': 'executed', 'create_dirs': True}
        elif step['id'] in actions:
            del step['run']
            step['action'] = 'js.run'
            step['with'] = {'script': actions[step['id']]}
    graph = {key: pipeline[key] for key in ('type', 'shell', 'artifacts', 'params', 'max_active_steps')}
    graph['steps'] = steps
    params = [f'commit={A}', f'ref={plugin_ref}']
    if mode is not None:
        params.append(f'native_mode={mode}')
    filenames = ('source-evidence.json', 'github-mirror-evidence.json', 'github-native/summary.json')
    before = {filename: set(home.rglob(filename)) for filename in filenames}
    result = run(name, graph, success, run_id=name, params=params)
    assert {path.name for path in marker_dir.glob('*')} == (containers if success else set())
    source, github_mirror, github_native = [set(home.rglob(filename)) - before[filename] for filename in filenames]
    assert len(source) == expect_source, source
    assert len(github_mirror) == expect_mirror, github_mirror
    if success and mode != 'async':
        assert len(github_native) == 1 and json.loads(github_native.pop().read_text()) == {'status': 'fixture'}
    else:
        # Dagu can create a stdout destination before skipping the step.
        assert all(path.stat().st_size == 0 for path in github_native), github_native
    if success and mode == 'async':
        assert 'github_native [skipped]' in result.stdout
    if expect_source:
        evidence = json.loads(source.pop().read_text())
        assert set(evidence) == {'schemaVersion', 'dagName', 'dagRunId', 'attemptId', 'startedAt',
                                 'sha', 'ref', 'sourceSHA'}
        assert evidence['schemaVersion'] == 1 and evidence['dagName'] == 'robotgo-ovh-ci'
        assert evidence['dagRunId'] == name and evidence['ref'] == plugin_ref
        assert evidence['sha'] == evidence['sourceSHA'] == A
        assert re.fullmatch(r'[A-Za-z0-9_-]+', evidence['attemptId']) and evidence['startedAt'].startswith('20')
    if expect_mirror:
        assert json.loads(github_mirror.pop().read_text()) == {
            'schema_version': 1, 'dag_run_id': name, 'attempt_id': evidence['attemptId'],
            'sha': A, 'tree': C, 'ref': plugin_ref,
        }

source_fixture('ed-combined-source-fixture')
source_fixture('ed-async-source-fixture', mode='async', expect_mirror=True)
source_fixture('ed-mismatched-source-fixture', mode='async', checkout_sha=B,
               success=False, expect_source=False)
source_fixture('ed-mismatched-mirror-fixture', mode='async', success=False,
               mirror={'source': B, 'tree': C, 'branch': 'ci/entire/' + A})
source_fixture('ed-invalid-mirror-tree-fixture', mode='async', success=False,
               mirror={'source': A, 'tree': '0' * 40, 'branch': 'ci/entire/' + A})
source_fixture('ed-invalid-mirror-branch-fixture', mode='async', success=False,
               mirror={'source': A, 'tree': C, 'branch': 'ci/entire/' + B})
source_fixture('ed-invalid-mode-fixture', mode='invalid', success=False, expect_source=False)

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
