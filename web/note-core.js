'use strict';
// Pure document/state helpers also exercised by Node tests.
(function(root) {
  const clone = value => JSON.parse(JSON.stringify(value));
  const uuid = () => {
    if(root.crypto.randomUUID)return root.crypto.randomUUID();
    const bytes=root.crypto.getRandomValues(new Uint8Array(16));bytes[6]=(bytes[6]&15)|64;bytes[8]=(bytes[8]&63)|128;
    const hex=Array.from(bytes,n=>n.toString(16).padStart(2,'0')).join('');
    return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
  };
  const day = d => `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;
  const parseDay = s => { const [y,m,d]=s.slice(0,10).split('-').map(Number);return new Date(y,m-1,d); };
  const shift = (s,n) => {const d=parseDay(s);d.setDate(d.getDate()+n);return day(d);};
  const block = (type='paragraph',text='') => ({id:uuid(),type,...(type==='heading'?{level:2}:{}),...(type==='query'?{query_mode:'detail',title:'查询',definition:null,view:{type:'table',mapping:{},options:{}}}:{text})});
  function queryBlock(name,title,view='table') {
    const b=block('query');b.title=title;b.query_mode=name==='time_stats'?'aggregate':'detail';b.definition={kind:'function',name,version:1,params:{range:{mode:'inherit'},tag_ids:{mode:'inherit'}}};
    if(name==='todos') { b.definition.params.date_field='created_at';b.definition.params.status='all'; }
    if(name==='time_stats') b.definition.params.group_by='tag';
    b.view={type:view,mapping:name==='todos'?{id:'id',title:'title',date:'created_at'}:name==='time_entries'?{id:'id',title:'title',start:'start_time',end:'end_time'}:{},options:view==='timeline'?{scale:'week'}:{}};
    return b;
  }
  function template(kind,serverToday) {
    let start=serverToday;let end=shift(start,1);
    if(kind!=='daily'){const d=parseDay(start);start=shift(start,-(d.getDay()+6)%7);end=shift(start,7);}
    const doc={schema_version:2,title:kind==='blank'?'':`${serverToday} ${kind==='daily'?'日报':'周报'}`,context:{range:{mode:'fixed',start,end_exclusive:end},tag_ids:[]},blocks:[block()]};
    if(kind==='blank')return doc;
    doc.blocks=[block('heading','工作反思'),block(),queryBlock('todos','本期创建的任务'),queryBlock('time_entries','时间记录','timeline'),block('heading',kind==='daily'?'明日计划':'下周计划'),block()];
    doc.blocks[3].view.options.scale=kind==='daily'?'day':'week';
    if(kind==='weekly')doc.blocks.splice(3,0,queryBlock('time_stats','按记录标签统计工时'));
    return doc;
  }
  const mode = definition => definition?.name==='time_stats'||definition?.query?.group_by?.length||definition?.query?.select?.some(s=>s.aggregate)?'aggregate':'detail';
  function upgrade(document) {
    if(![1,2].includes(document.schema_version))throw Error('不支持的笔记版本，请升级应用');
    document.schema_version=2;
    for(const b of document.blocks)if(b.type==='query'){
      b.query_mode ||= mode(b.definition);
      const q=b.definition?.query;
      if(q){q.select ||= [];q.joins ||= [];q.group_by ||= [];q.order_by ||= [];q.limit ||= 50;q.offset ||= 0;}
    }
    return document;
  }
  function dataBlock(view='table',source=view==='timeline'?'time_entries':'todos') {
    const b=block('query');b.title={table:'数据表格',calendar:'任务日历',timeline:'时间记录时间轴'}[view];
    const query={source,select:[],joins:[],group_by:[],order_by:[],limit:50,offset:0};
    let mapping={};
    if(view==='calendar'){query.select=['id','title','due_date'].map(field=>({field}));mapping={id:'id',title:'title',date:'due_date'};}
    if(view==='timeline'){query.select=['id','note','start_time','end_time'].map(field=>({field}));mapping={id:'id',title:'note',start:'start_time',end:'end_time'};}
    b.definition={kind:'custom',query};b.view={type:view,mapping,options:view==='timeline'?{scale:'week'}:{}};return b;
  }
  function dependsOn(b,key) {
    const d=b.definition;if(!d)return false;
    if(d.kind==='custom')return (d.bindings||[]).some(binding=>binding.source==='context'&&binding.type===(key==='range'?'range':'tag_ids'));
    return key==='range'?(!d.params?.range?.mode||d.params.range.mode==='inherit'):(!d.params?.tag_ids?.mode||d.params.tag_ids.mode==='inherit');
  }
  function filterProblem(p,fields,depth=1,root=true,budget={leaves:0}) {
    if(!p)return '';
    if(depth>5)return '筛选最多嵌套 5 层';
    if(p.not)return filterProblem(p.not,fields,depth+1,false,budget);
    const children=p.and||p.or;
    if(children){if(children.length>20)return '每组最多 20 个条件';if(!children.length)return root?'':'条件组尚未完成';for(const c of children){const error=filterProblem(c,fields,depth+1,false,budget);if(error)return error;}
      if(p.and?.length===2){const lower=p.and.find(c=>['gt','gte'].includes(c.op)),upper=p.and.find(c=>['lt','lte'].includes(c.op));if(lower&&upper&&lower.field===upper.field&&lower.value>upper.value)return '范围下限不能大于上限';}
      return '';}
    budget.leaves+=p.op==='in_relative_range'?2:1;if(budget.leaves>20)return '最多 20 个筛选条件';
    const f=fields.find(f=>f.key===p.field);if(!f)return '请选择合法的筛选字段';
    if(![...(f.operators||[]),...(f.note_filter_ops||[])].includes(p.op))return '请选择该字段支持的操作符';
    if(['is_empty','is_not_empty'].includes(p.op))return '';
    if(p.value===''||p.value==null)return '请填写筛选值';
    if(p.op==='in_relative_range')return ['today','this_week','this_month','past_7_days'].includes(p.value)?'':'请选择相对日期';
    const array=['in','between'].includes(p.op);if(array&&(!Array.isArray(p.value)||!p.value.length||(p.op==='between'&&p.value.length!==2)))return '请填写完整范围或集合';
    const values=array?p.value:[p.value];if(values.length>100)return '集合最多 100 个值';for(const value of values){if(value===''||value==null)return '范围值尚未完成';if(f.type==='number'&&(typeof value!=='number'||!Number.isFinite(value)))return '请输入合法数值';if(f.type==='boolean'&&typeof value!=='boolean')return '请选择是或否';}
    if(p.op==='between'&&p.value[0]>p.value[1])return '范围下限不能大于上限';
    return '';
  }
  function draftProblem(b,catalog) {
    if(!b.definition)return '请选择查询数据源或模板';
    if(b.query_mode!==mode(b.definition))return b.query_mode==='detail'?'明细模式不能包含 SQL 聚合，请切换聚合报表':'请配置聚合或聚合分组';
    if(b.query_mode==='aggregate'&&(b.view.options.group||b.view.options.summaries?.length))return '聚合报表不能使用明细分组或列底统计';
    const d=b.definition;if(d.kind!=='custom')return '';
    const q=d.query,source=catalog.sources.find(s=>s.key===q?.source);if(!source)return '请选择合法数据源';
    if(!Number.isInteger(q.limit)||q.limit<1||q.limit>500)return '每页条数须为 1–500 的整数';
    const fields=source.fields.filter(f=>!f.key.includes('.')||q.joins?.some(j=>j.relation===f.key.split('.')[0]));
    for(const s of q.select||[])if(!s.field&&s.aggregate!=='count')return '返回字段尚未完成';
    if(q.group_by?.some(g=>!g))return '聚合分组尚未完成';
    if(q.order_by?.some(o=>!o.key||!['asc','desc'].includes(o.direction)))return '排序条件尚未完成';
    const budget={leaves:0},error=filterProblem(q.where,fields,1,true,budget);if(error)return error;
    if(d.search){
      if(d.search.text.length>200||d.search.fields?.length>20)return '搜索关键字或字段超过限制';
      const selected=d.search.fields?.length?d.search.fields:fields.filter(f=>f.type==='text').map(f=>f.key);
      if(selected.some(key=>!fields.some(f=>f.key===key&&f.type==='text')))return '搜索需要合法的文本字段';
      if(d.search.text&&budget.leaves+selected.length>20)return '筛选与搜索编译后最多 20 个条件';
    }
    return '';
  }
  function tableProblem(b,outputs) {
    if(b.view.type!=='table'||b.query_mode!=='detail')return '';
    const group=b.view.options.group,metrics=b.view.options.summaries||[];
    if(group&&(!outputs.some(c=>c.key===group.field&&c.groupable!==false)||!['asc','desc'].includes(group.direction)))return '请选择合法的明细分组字段与顺序';
    if(metrics.length>10)return '每块最多 10 个统计指标';
    const ids=new Set();
    for(const m of metrics){
      if(!m.id||m.id.length>64||ids.has(m.id))return '统计指标 ID 必须唯一且不超过 64 字符';ids.add(m.id);
      if(m.op==='count_all'&&!m.field)continue;
      const c=outputs.find(c=>c.key===m.field);
      if(!c||!c.summary_ops?.includes(m.op))return '统计字段或操作不合法，请修复指标';
    }
    return '';
  }
  function executionDefinition(b) {
    const d=clone(b.definition),p=d.query?.where;
    // Only an explicitly empty root means no filter. Incomplete child conditions stay invalid.
    if(p&&((p.and&&p.and.length===0)||(p.or&&p.or.length===0)))delete d.query.where;
    return d;
  }
  class Preview {
    constructor(run,delay=400,timers=root){this.run=run;this.delay=delay;this.timers=timers;this.composing=false;}
    schedule(){this.cancel();if(!this.composing)this.timer=this.timers.setTimeout(()=>{this.timer=null;this.run();},this.delay);}
    cancel(){if(this.timer!=null)this.timers.clearTimeout(this.timer);this.timer=null;}
    pause(){this.composing=true;this.cancel();}
    resume(){this.composing=false;this.schedule();}
  }
  class SaveQueue {
    constructor({id=null,revision=0,document,write,onStatus=()=>{},draft=()=>{},delay=800}) {
      Object.assign(this,{id,revision,document,write,onStatus,draft,delay});this.version=0;this.saved=0;this.conflict=false;this.running=null;this.timer=null;
    }
    changed(){this.version++;this.draft(this);this.onStatus('待保存');clearTimeout(this.timer);if(!this.conflict)this.timer=setTimeout(()=>this.flush(),this.delay);}
    get dirty(){return this.version!==this.saved;}
    async flush(){
      clearTimeout(this.timer);if(this.running)return this.running;if(this.conflict||!this.dirty)return;
      this.running=(async()=>{
        while(this.dirty&&!this.conflict){const version=this.version;const snapshot=clone(this.document);this.onStatus('保存中');
          try{const note=await this.write({id:this.id,revision:this.revision,document:snapshot});this.id=note.id;this.revision=note.revision;this.saved=version;this.draft(this);this.onStatus(this.dirty?'待保存':'已保存',note);}
          catch(err){this.conflict=err.status===409;this.onStatus(this.conflict?'版本冲突':'保存失败',null,err);break;}
        }
      })();try{await this.running;}finally{this.running=null;}
    }
    stop(){clearTimeout(this.timer);}
  }
  function updateDraft(storage,q) {
    const key=`todo-note-draft-${q.id||'new'}`;
    q.draftToken ||= uuid();
    const removeOwned=key=>{const raw=storage.getItem(key);if(raw&&JSON.parse(raw).token===q.draftToken)storage.removeItem(key);};
    if(q.dirty)storage.setItem(key,JSON.stringify({token:q.draftToken,id:q.id,revision:q.revision,document:q.document,time:new Date().toISOString()}));
    else removeOwned(key);
    if(q.id)removeOwned('todo-note-draft-new');
  }
  class Scheduler {
    constructor(limit=2){this.limit=limit;this.active=0;this.pending=[];}
    add(fn){return new Promise((resolve,reject)=>{this.pending.push({fn,resolve,reject});this.drain();});}
    drain(){while(this.active<this.limit&&this.pending.length){const job=this.pending.shift();this.active++;Promise.resolve().then(job.fn).then(job.resolve,job.reject).finally(()=>{this.active--;this.drain();});}}
  }
  // Split wall-clock intervals by local calendar day, independent of the browser timezone.
  function pieces(rows,mapping,asOf,range) {
    const result=[];let missing=0;
    for(const row of rows){
      const start=row[mapping.date||mapping.start];if(!start){missing++;continue;}
      const end=mapping.end?(row[mapping.end]||asOf?.slice(0,19).replace('T',' ')):null;
      const first=String(start).slice(0,10);if(!/^\d{4}-\d{2}-\d{2}$/.test(first)||day(parseDay(first))!==first){missing++;continue;}
      let last=end?String(end).slice(0,10):first;
      if(end&&String(end).slice(11,19)==='00:00:00')last=shift(last,-1);
      if(last<first)continue;
      let current=range?.start&&first<range.start?range.start:first;
      for(let count=0;current<=last&&count<3660;current=shift(current,1),count++){
        if(range?.end_exclusive&&current>=range.end_exclusive)break;
        const from=current===first?(String(start).slice(11,19)||'00:00:00'):'00:00:00';
        const to=end&&current===String(end).slice(0,10)?String(end).slice(11,19):'24:00:00';
        result.push({row,date:current,from,to,title:String(row[mapping.title]??'未命名'),id:row[mapping.id]});
      }
    }
    return {items:result,missing};
  }
  function lanes(items){
    const minute=s=>Number(s.slice(0,2))*60+Number(s.slice(3,5));
    const sorted=items.map(x=>({...x,start:minute(x.from),end:minute(x.to)})).sort((a,b)=>a.start-b.start||a.end-b.end);
    let group=[],ends=[],max=0,groupEnd=-1;
    const finish=()=>{for(const x of group)x.lanes=max;};
    for(const x of sorted){if(x.start>=groupEnd){finish();group=[];ends=[];max=0;groupEnd=-1;}let lane=ends.findIndex(end=>end<=x.start);if(lane<0)lane=ends.length;ends[lane]=x.end;x.lane=lane;group.push(x);max=Math.max(max,ends.length);groupEnd=Math.max(groupEnd,x.end);}
    finish();return sorted;
  }
  root.NoteCore={clone,uuid,day,parseDay,shift,block,queryBlock,template,mode,upgrade,dataBlock,dependsOn,filterProblem,draftProblem,tableProblem,executionDefinition,Preview,SaveQueue,updateDraft,Scheduler,pieces,lanes};
  if(typeof module!=='undefined')module.exports=root.NoteCore;
})(globalThis);
