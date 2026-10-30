'use strict';
const NoteApp = (() => {
  const C=NoteCore;
  const app={functions:[],catalog:null,views:[],today:'',timezone:'',items:[],total:0,offset:0,deleted:false,search:'',current:null,generation:0,library:false,error:'',listToken:0};
  const button=(label,fn,disabled=false)=>{const n=el('button',{type:'button',class:'btn btn-small',onclick:()=>Promise.resolve().then(fn).catch(showError)},label);n.disabled=disabled;return n;};
  const textInput=(label,value,change,type='text')=>{const n=el('input',{class:'input',type,'aria-label':label,placeholder:label,oninput:e=>change(e.target.value)});n.value=value??'';return n;};
  const select=(label,options,value,change)=>{const n=el('select',{class:'select','aria-label':label,onchange:e=>change(e.target.value)},...options.map(o=>el('option',{value:typeof o==='string'?o:o.key},typeof o==='string'?o:o.label)));n.value=value??'';return n;};
  async function request(method,path,body,signal){
    const response=await fetch(path,{method,headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal});
    if(response.status===204)return null;let data;try{data=await response.json();}catch{data={message:'服务器响应无效'};}
    if(!response.ok){const err=new Error(`${data.path?data.path+': ':''}${data.message||data.error||response.statusText}`);err.status=response.status;err.code=data.code;throw err;}return data;
  }
  function showError(error){app.error=error.message||String(error);const host=$('#notes-error');if(host){host.textContent=app.error;host.classList.remove('hidden');}}
  function clearError(){app.error='';const n=$('#notes-error');if(n){n.textContent='';n.classList.add('hidden');}}
  function dirty(){return app.current?.queue.dirty||app.current?.queue.conflict;}
  function canLeave(){if(!dirty())return true;return confirm('笔记尚未保存。恢复草稿会保留在浏览器中，是否继续离开？');}
  function cancelBlocks(session=app.current){if(session?.runtimes)for(const rt of session.runtimes.values()){rt.sequence++;rt.preview?.cancel();rt.controller?.abort();rt.observer?.disconnect();}}
  function saveDraft(q){
    try{
      C.updateDraft(localStorage,q);
    }catch{if(app.current?.queue===q){app.current.storageError='浏览器草稿存储不可用；请保持页面打开并手动保存。';updateStatus();}}
  }
  function statusText(s){return s.status+(s.savedAt?` · ${s.savedAt}`:'')+(s.storageError?' · '+s.storageError:'');}
  function updateStatus(){const s=app.current;if(!s)return;const n=$('#note-save-status');if(n)n.textContent=statusText(s);const conflicts=$('#note-conflict');if(conflicts)conflicts.classList.toggle('hidden',!s.queue.conflict);const retry=$('#note-retry');if(retry)retry.classList.toggle('hidden',s.status!=='保存失败');}
  function session(note,isNew=false,recovered=null){
    const s={id:note.id||null,document:C.upgrade(recovered?.document||note.document),status:'已保存',savedAt:'',storageError:'',undo:[],runtimes:new Map(),hosts:new Map()};
    s.queue=new C.SaveQueue({id:s.id,revision:recovered?.revision??note.revision??0,document:s.document,write:({id,revision,document})=>request(id?'PUT':'POST',id?`/api/notes/${id}`:'/api/notes',id?{revision,document}:{document}),draft:saveDraft,onStatus:(status,saved,err)=>{
      s.status=status;if(saved){s.id=saved.id;s.savedAt=new Date(saved.updated_at).toLocaleTimeString('zh-CN');if(app.current===s){clearError();history.replaceState(null,'',`#summary/note/${s.id}`);const del=$('#note-delete');if(del)del.disabled=false;refreshList().catch(showError);}}
      if(app.current===s){updateStatus();if(err)showError(err);}
    }});
    if(isNew||recovered){s.queue.version=1;s.status='待保存';}
    return s;
  }
  function recover(id){try{const raw=localStorage.getItem(`todo-note-draft-${id||'new'}`);if(!raw)return null;const d=JSON.parse(raw);if(!d.document||![1,2].includes(d.document.schema_version))throw Error();if(confirm(`发现 ${d.time||''} 的未保存草稿，是否恢复？取消将丢弃这份浏览器草稿。`))return d;localStorage.removeItem(`todo-note-draft-${id||'new'}`);}catch{showError(new Error('恢复草稿读取失败，服务器笔记未改变。'));}return null;}
  function activate(note,isNew=false,recovered=null){
    app.current?.queue.stop();cancelBlocks();app.generation++;app.current=session(note,isNew,recovered);app.library=false;app.error='';render();
    if(app.current.queue.dirty)app.current.queue.changed();
  }
  async function open(id,force=false){
    if(!force&&app.current?.id===Number(id)){app.library=false;render();return;}
    if(!force&&!canLeave()){if(app.current?.id)history.replaceState(null,'',`#summary/note/${app.current.id}`);return;}
    const token=++app.generation;cancelBlocks();const note=await request('GET',`/api/notes/${id}`);if(token!==app.generation)return;activate(note,false,recover(id));history.replaceState(null,'',`#summary/note/${id}`);
  }
  function create(kind='blank'){
    if(!canLeave())return;activate({document:C.template(kind,app.today)},true,kind==='blank'?recover(null):null);history.replaceState(null,'','#summary');
  }
  async function refreshList(append=false){
    const token=++app.listToken;const offset=append?app.items.length:0;
    const result=await request('GET',`/api/notes?limit=50&offset=${offset}&deleted=${app.deleted}&search=${encodeURIComponent(app.search)}`);if(token!==app.listToken)return;
    app.items=append?app.items.concat(result.items):result.items;app.total=result.total_count;renderList();
  }
  function renderList(){
    const host=$('#note-list');if(!host)return;host.replaceChildren();
    if(!app.items.length)host.appendChild(emptyHint(app.deleted?'回收站为空':'尚无笔记'));
    for(const n of app.items){const row=el('div',{class:'note-list-row'});const b=button(n.title,()=>app.deleted?restore(n):open(n.id));b.title=`${n.title}\n${n.updated_at}`;if(n.id===app.current?.id)b.setAttribute('aria-current','true');row.appendChild(b);if(app.deleted)row.appendChild(el('small',{},'点击恢复'));host.appendChild(row);}
    if(app.items.length<app.total)host.appendChild(button('加载更多',()=>refreshList(true)));
  }
  async function restore(n){const saved=await request('POST',`/api/notes/${n.id}/restore`,{revision:n.revision});app.deleted=false;await refreshList();await open(saved.id);}
  async function remove(){const s=app.current;if(!s?.id||!confirm('将这篇笔记移到回收站？'))return;await s.queue.flush();if(s.queue.dirty||s.queue.conflict){showError(new Error('请先保存或处理冲突，再删除笔记。'));return;}await request('DELETE',`/api/notes/${s.id}`,{revision:s.queue.revision});cancelBlocks();s.queue.stop();app.current=null;history.replaceState(null,'','#summary');await refreshList();render();}
  function changed(){const s=app.current;if(!s)return;s.queue.changed();}
  function contextChanged(key='range'){changed();for(const b of app.current.document.blocks)if(b.type==='query'&&C.dependsOn(b,key))QueryBlocks.invalidate(b);}
  function pushUndo(){const s=app.current;s.undo.push(C.clone(s.document.blocks));if(s.undo.length>20)s.undo.shift();}
  function mutate(fn){pushUndo();fn(app.current.document.blocks);changed();syncBlocks();}
  function undo(){const s=app.current;if(!s.undo.length)return;const current=new Map(s.document.blocks.map(b=>[b.id,b]));s.document.blocks=s.undo.pop().map(b=>current.get(b.id)||b);changed();syncBlocks();}
  function autosize(n){n.style.height='auto';n.style.height=`${Math.max(n.scrollHeight,42)}px`;}
  function textBlock(b){
    const n=el('textarea',{class:`note-text note-${b.type}`,'aria-label':b.type==='heading'?'标题块':b.type==='bullet'?'列表项':'段落',rows:'1',placeholder:b.type==='paragraph'?'输入文字，或在空白处输入 / 插入内容':'输入内容',oninput:()=>{b.text=n.value;changed();autosize(n);},onkeydown:e=>{
      if(e.isComposing||e.keyCode===229)return;
      if(e.key==='/'&&!n.value){e.preventDefault();insertMenu(b.id,n,true);}
    }});n.value=b.text||'';if(b.type==='heading')n.dataset.level=b.level;requestAnimationFrame(()=>autosize(n));return n;
  }
  function blockHost(b){
    const host=el('section',{class:`note-block note-block-${b.type}`,'data-block-id':b.id});const tools=el('div',{class:'note-block-tools','aria-label':'块操作'});
    tools.append(button('↑',()=>mutate(items=>{const i=items.indexOf(b);if(i>0)[items[i-1],items[i]]=[items[i],items[i-1]];})),button('↓',()=>mutate(items=>{const i=items.indexOf(b);if(i<items.length-1)[items[i],items[i+1]]=[items[i+1],items[i]];})),button('复制',()=>mutate(items=>{const copy=C.clone(b);copy.id=C.uuid();items.splice(items.indexOf(b)+1,0,copy);})),button('删除',()=>mutate(items=>{items.splice(items.indexOf(b),1);if(!items.length)items.push(C.block());})));
    host.appendChild(tools);if(b.type==='query')host.appendChild(QueryBlocks.mount(b));else host.appendChild(textBlock(b));
    const add=button('＋ 插入',()=>insertMenu(b.id,add));add.classList.add('note-add');host.appendChild(add);return host;
  }
  function syncBlocks(){
    const s=app.current;const body=$('#note-blocks');if(!s||!body)return;const ids=new Set(s.document.blocks.map(b=>b.id));
    for(const [id,host] of s.hosts)if(!ids.has(id)){const rt=s.runtimes.get(id);if(rt){rt.sequence++;rt.preview?.cancel();rt.controller?.abort();rt.observer?.disconnect();s.runtimes.delete(id);}host.remove();s.hosts.delete(id);}
    s.document.blocks.forEach((b,index)=>{let host=s.hosts.get(b.id);if(!host){host=blockHost(b);s.hosts.set(b.id,host);}const existing=body.children[index];if(existing!==host)body.insertBefore(host,existing||null);});
  }
  function insertMenu(afterId,anchor,replaceEmpty=false){
    document.querySelector('.note-insert-menu')?.remove();const menu=el('div',{class:'note-insert-menu',role:'dialog','aria-label':'插入块'});const search=textInput('搜索块类型','',()=>filter());menu.appendChild(search);const list=el('div',{role:'menu'});menu.appendChild(list);
    const choices=[['段落',()=>C.block()],['标题 1',()=>({...C.block('heading'),level:1})],['标题 2',()=>C.block('heading')],['标题 3',()=>({...C.block('heading'),level:3})],['列表项',()=>C.block('bullet')],['表格',()=>C.dataBlock('table')],['日历',()=>C.dataBlock('calendar')],['时间轴',()=>C.dataBlock('timeline')],...app.functions.map(f=>[f.label,()=>C.queryBlock(f.name,f.label)]),['高级查询',()=>({...C.block('query'),title:'高级查询',definition:{kind:'custom',query:newSummaryQuery('todos')}})],...app.views.map(v=>[`已保存查询：${v.name}`,()=>({...C.block('query'),title:v.name,query_mode:C.mode({query:v.query}),definition:{kind:'custom',query:C.clone(v.query),origin_view_id:v.id}})])];
    function filter(){list.replaceChildren();for(const [label,make] of choices.filter(([label])=>label.includes(search.value))){const b=button(label,()=>{mutate(items=>{const i=items.findIndex(b=>b.id===afterId);const empty=items[i];const next=make();if(replaceEmpty&&empty?.type!=='query'&&!empty?.text)items.splice(i,1,next);else items.splice(i+1,0,next);});menu.remove();});b.setAttribute('role','menuitem');list.appendChild(b);}}
    menu.addEventListener('keydown',e=>{if(e.isComposing)return;if(e.key==='Escape'){menu.remove();anchor.focus();}if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();const nodes=[search,...list.querySelectorAll('button')];const i=nodes.indexOf(document.activeElement);nodes[(i+(e.key==='ArrowDown'?1:-1)+nodes.length)%nodes.length].focus();}if(e.key==='Enter'){e.preventDefault();const target=document.activeElement===search?list.querySelector('button'):document.activeElement;if(list.contains(target))target?.click();}});
    const close=e=>{if(!menu.contains(e.target)&&e.target!==anchor){menu.remove();document.removeEventListener('pointerdown',close);}};document.addEventListener('pointerdown',close);anchor.parentNode.appendChild(menu);filter();search.focus();
  }
  function renderContext(s){
    const details=el('details',{class:'note-context'});details.appendChild(el('summary',{},'笔记日期与标签'));const box=el('div',{class:'summary-row'});details.appendChild(box);
    const r=s.document.context.range;
    box.appendChild(select('日期模式',[{key:'fixed',label:'固定日期'},{key:'relative',label:'动态日期'},{key:'none',label:'不限制日期'}],r.mode,value=>{s.document.context.range=value==='fixed'?{mode:value,start:app.today,end_exclusive:C.shift(app.today,1)}:value==='relative'?{mode:value,preset:'this_week'}:{mode:value};contextChanged();const next=renderContext(s);next.open=details.open;details.replaceWith(next);}));
    if(r.mode==='fixed'){box.append(textInput('笔记开始日期',r.start,value=>{r.start=value;contextChanged();},'date'),textInput('笔记结束日期（含）',r.end_exclusive?C.shift(r.end_exclusive,-1):'',value=>{r.end_exclusive=value?C.shift(value,1):'';contextChanged();},'date'));}
    if(r.mode==='relative')box.appendChild(select('动态范围',[{key:'today',label:'今天'},{key:'this_week',label:'本周'},{key:'this_month',label:'本月'}],r.preset,v=>{r.preset=v;contextChanged();}));
    const tags=el('div',{class:'note-tag-choices'});for(const tag of state.tags){const input=el('input',{type:'checkbox',onchange:()=>{const ids=s.document.context.tag_ids;const i=ids.indexOf(tag.id);if(input.checked&&i<0)ids.push(tag.id);else if(!input.checked&&i>=0)ids.splice(i,1);contextChanged('tag_ids');}});input.checked=s.document.context.tag_ids.includes(tag.id);tags.appendChild(el('label',{},input,tag.name));}details.append(tags,el('small',{class:'muted'},`业务时间采用服务器时区 ${app.timezone}。固定日期仍展示最新数据。`));return details;
  }
  function render(){
    const root=$('#notes-content');if(!root)return;root.classList.remove('hidden');$('#summary-library')?.classList.add('hidden');root.replaceChildren();
    const aside=el('aside',{class:'card note-sidebar'},el('h3',{},'总结笔记'));aside.append(button('＋ 空白笔记',()=>create()),button('日报',()=>create('daily')),button('周报',()=>create('weekly')),button('查询库',openLibrary),button('导入旧总结',importDialog),button(app.deleted?'返回笔记':'回收站',async()=>{app.deleted=!app.deleted;await refreshList();render();}));
    let searchTimer;aside.appendChild(textInput('搜索笔记标题',app.search,value=>{app.search=value;clearTimeout(searchTimer);searchTimer=setTimeout(()=>refreshList().catch(showError),250);}));aside.appendChild(el('div',{id:'note-list'}));
    const main=el('div',{class:'note-main'});const error=el('div',{id:'notes-error',class:'summary-error'+(app.error?'':' hidden'),role:'alert'},app.error);main.appendChild(error);
    root.appendChild(el('div',{class:'note-layout'},aside,main));renderList();
    const s=app.current;if(!s){main.appendChild(el('div',{class:'card note-welcome'},el('h2',{},'文字与数据，写在同一篇总结里'),el('p',{},'创建笔记，在段落之间插入任务、工时统计、日历或时间轴。'),button('新建周报',()=>create('weekly'))));return;}
    s.hosts.clear();cancelBlocks(s);s.runtimes.clear();
    const bar=el('div',{class:'note-toolbar'},el('span',{id:'note-save-status',role:'status'},statusText(s)),button('保存',()=>s.queue.flush()),button('撤销块操作',undo),button('刷新全部',()=>QueryBlocks.refreshAll()),button('另存副本',saveCopy));const del=button('移到回收站',remove,!s.id);del.id='note-delete';bar.appendChild(del);main.appendChild(bar);
    const retry=button('重试保存',()=>s.queue.flush());retry.id='note-retry';retry.classList.add('hidden');main.appendChild(retry);
    const conflict=el('div',{id:'note-conflict',class:'summary-error hidden'},'服务器已有新版本。你的草稿仍然保留。',button('另存本地稿',saveCopy),button('重新载入',()=>{if(confirm('放弃当前编辑并加载服务器版本？')){try{localStorage.removeItem(`todo-note-draft-${s.id}`);}catch{}return open(s.id,true);}}));main.appendChild(conflict);
    const title=textInput('笔记标题',s.document.title,value=>{s.document.title=value;changed();});title.classList.add('note-title');title.placeholder='未命名笔记';main.append(title,renderContext(s),el('div',{id:'note-blocks'}));syncBlocks();updateStatus();
  }
  async function saveCopy(){const s=app.current;if(!s)return;const document=C.clone(s.document);document.title=(document.title||'未命名笔记')+' 副本';document.blocks.forEach(b=>{b.id=C.uuid();});const n=await request('POST','/api/notes',{document});activate(n);history.replaceState(null,'',`#summary/note/${n.id}`);await refreshList();}
  async function openLibrary(){app.library=true;$('#notes-content').classList.add('hidden');const host=$('#summary-library');host.classList.remove('hidden');if(!$('#note-library-back')){const b=button('← 返回笔记',async()=>{app.views=await request('GET','/api/query/views');app.library=false;render();});b.id='note-library-back';host.prepend(b);}await loadSummary();}
  function importDialog(){
    const content=el('div',{class:'stack'});const kind=select('旧总结类型',[{key:'daily',label:'日报'},{key:'weekly',label:'周报'}],'daily',()=>{});const key=textInput('日期或 ISO 周',app.today,()=>{});const preview=el('pre',{class:'note-import-preview'});let valid=null;
    content.append(kind,key,el('small',{},'日报格式 YYYY-MM-DD，周报格式 YYYY-Www；复制原正文，来源保持不变。'),button('预览原文',async()=>{const source=await request('GET',`/api/summaries/${kind.value}?${kind.value==='daily'?'date':'week'}=${encodeURIComponent(key.value)}`);preview.textContent=source.content||'（空正文）';valid={kind:kind.value,key:key.value};}),preview,button('复制为笔记',async()=>{if(!valid||valid.kind!==kind.value||valid.key!==key.value)throw new Error('请先预览所选日期的正文。');if(!canLeave())return;const n=await request('POST','/api/notes/import-summary',valid);closeModal();if(n.deleted_at){app.deleted=true;await refreshList();render();showError(new Error('该总结已导入，笔记位于回收站。请点击恢复。'));}else{await open(n.id);await refreshList();}}));openModal('导入旧总结',content);
  }
  async function show(){
    try{
      if(!app.catalog){const [functions,catalog,views]=await Promise.all([request('GET','/api/note-functions'),request('GET','/api/query/catalog'),request('GET','/api/query/views')]);app.functions=functions.functions;app.today=functions.as_of.slice(0,10);app.timezone=functions.timezone;app.catalog=catalog;app.views=views;}
      const id=location.hash.match(/^#summary\/note\/(\d+)$/)?.[1];if(id&&Number(id)!==app.current?.id){render();await open(id);}else if(!app.library){render();if(!app.current){const draft=recover(null);if(draft)activate({document:draft.document},false,draft);}}
      await refreshList();
    }catch(err){render();showError(err);}
  }
  window.addEventListener('beforeunload',e=>{if(dirty()){e.preventDefault();e.returnValue='';}});
  window.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='s'&&state.view==='summary'&&!app.library){e.preventDefault();app.current?.queue.flush();}});
  window.addEventListener('hashchange',()=>{if(location.hash.startsWith('#summary')){if(state.view!=='summary')renderView('summary');else show();}});
  return {app,button,textInput,select,request,showError,changed,contextChanged,show,canLeave,cancelBlocks,syncBlocks,clearError};
})();
