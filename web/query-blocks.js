'use strict';
const QueryBlocks = (() => {
  const N = NoteApp, C = NoteCore, scheduler = new C.Scheduler(2);
  const labels = {created_at:'创建时间',due_date:'截止日期',completed_at:'完成时间',any:'任一标签',all:'全部',pending:'未开始',in_progress:'进行中',done:'已完成',total:'总计',todo:'任务',tag:'记录标签',day:'日期'};
  const metricLabels = {count_all:'总行数',count_values:'已填写数量',count_not_empty:'非空数量',count_empty:'未填写数量',count_distinct:'去重计数',percent_empty:'未填写比例',percent_not_empty:'已填写比例',sum:'求和',avg:'平均值',min:'最小值',max:'最大值'};
  const runtime = b => N.app.current?.runtimes.get(b.id);
  const order = b => b.definition?.kind === 'custom' ? b.definition.query?.order_by || [] : b.definition?.order_by || [];
  const setOrder = (b, value) => { if (b.definition.kind === 'custom') b.definition.query.order_by = value; else b.definition.order_by = value; };

  function columns(b) {
    const d = b.definition;
    if (!d) return [];
    if (d.kind === 'function') return N.app.functions.find(f => f.name === d.name)?.columns || [];
    const source = N.app.catalog.sources.find(s => s.key === d.query?.source);
    const selection = d.query?.select?.length ? d.query.select : (source?.defaults || []).map(field => ({field}));
    return selection.map(item => {
      const f = source?.fields.find(f => f.key === item.field);
      const key = summaryOutputKey(item);
      return {key, label:item.alias || f?.label || item.field || key, type:item.aggregate ? 'number' : f?.type || 'text', summary_ops:f?.summary_ops, groupable:!item.aggregate};
    });
  }
  function visibleWindow(b, rt) {
    if (b.view.type === 'table') return undefined;
    let range;
    if (b.definition?.kind === 'function') {
      const p = b.definition.params?.range;
      range = p?.mode && p.mode !== 'inherit' ? p : N.app.current.document.context.range;
    } else if (C.dependsOn(b, 'range')) range = N.app.current.document.context.range;
    let date = rt.anchor || (range?.mode === 'fixed' ? range.start : N.app.today);
    if (!rt.anchor && b.definition?.kind === 'custom') {
      const find = p => {
        if (!p) return;
        if (['gte','gt','eq','between'].includes(p.op) && p.field === (b.view.mapping.date || b.view.mapping.start)) {
          const value = Array.isArray(p.value) ? p.value[0] : p.value;
          if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}/.test(value)) date = value.slice(0,10);
        }
        (p.and || p.or || []).forEach(find);
      };
      find(b.definition.query?.where);
    }
    if (!date) date = N.app.today;
    rt.displayDate = date;
    if (b.view.type === 'calendar') {
      const d = C.parseDay(date); d.setDate(1);
      const start = C.shift(C.day(d), -(d.getDay()+6)%7);
      return {start,end_exclusive:C.shift(start,42)};
    }
    const count = b.view.options.scale === 'day' ? 1 : 7;
    if (count === 7) date = C.shift(date, -(C.parseDay(date).getDay()+6)%7);
    return {start:date,end_exclusive:C.shift(date,count)};
  }
  function renderStatus(rt) {
    const messages = [rt.status];
    if (rt.response) messages.push(`实时数据 · ${rt.response.meta.as_of} · ${rt.response.meta.timezone}`);
    if (rt.stale) messages.push('结果已过期，下面为上次完整结果');
    if (rt.error) messages.push(rt.error);
    rt.statusHost.textContent = messages.filter(Boolean).join(' · ');
    rt.statusHost.classList.toggle('summary-error', !!rt.error);
    rt.resultHost.classList.toggle('note-stale', !!rt.stale);
    rt.resultHost.querySelectorAll('[data-query-page]').forEach(n => { n.disabled = rt.stale || n.dataset.unavailable === 'true'; });
  }
  function invalidate(b, auto = true) {
    const rt = runtime(b); if (!rt) return;
    rt.started = true; rt.observer?.disconnect(); rt.revision++; rt.sequence++; rt.controller?.abort();
    rt.offset = 0; rt.stale = !!rt.response; rt.error = '';
    rt.status = '配置已修改，等待预览';
    const problem = C.draftProblem(b,N.app.catalog) || C.tableProblem(b,columns(b));
    if (problem) { rt.error = problem; rt.status = '配置未完成'; rt.preview.cancel(); }
    else if (auto) rt.preview.schedule();
    renderStatus(rt);
  }
  function changed(b, redraw = false, auto = true) {
    N.changed(); invalidate(b,auto);
    if (redraw) renderConfig(b,runtime(b));
  }
  function renderResult(b, rt) {
    rt.resultHost.replaceChildren();
    if (rt.response && rt.applied) {
      const display = C.clone(rt.applied.block);
      // Column visibility is presentation-only; the underlying applied query stays immutable.
      display.view.options.columns = b.view.options.columns;
      rt.resultHost.appendChild(QueryViews.render(display,rt.response,{
        offset:rt.applied.page.offset,limit:rt.applied.page.limit,sort:rt.applied.order,stale:rt.stale,collapsed:rt.collapsed,
        onPage:offset => execute(b,offset),
        onSort:sort => { setOrder(b,sort); N.changed(); invalidate(b,false); execute(b,0); }
      }));
    } else rt.resultHost.appendChild(emptyHint('选择数据源并配置本块查询，完整条件将自动预览'));
    renderStatus(rt);
  }
  async function execute(b, offset = 0) {
    const session = N.app.current;
    if (!session?.document.blocks.includes(b)) return;
    const rt = runtime(b); if (!rt || rt.preview.composing) return;
    rt.preview.cancel();
    const problem = C.draftProblem(b,N.app.catalog) || C.tableProblem(b,columns(b));
    if (problem) { rt.error = problem; rt.status = '配置未完成'; rt.stale = !!rt.response; renderStatus(rt); return; }
    rt.started = true; rt.observer?.disconnect(); rt.controller?.abort();
    const sequence = ++rt.sequence, revision = rt.revision, controller = new AbortController(); rt.controller = controller;
    rt.status = '等待执行'; rt.error = ''; rt.offset = offset;
    const limit = b.definition.kind === 'custom' ? b.definition.query.limit || 50 : rt.limit;
    const snapshot = {block:C.clone(b),context:C.clone(session.document.context),page:{limit,offset},order:C.clone(order(b))};
    const request = {query_mode:b.query_mode,definition:C.executionDefinition(b),context:snapshot.context,view:snapshot.block.view,page:snapshot.page};
    const viewport = visibleWindow(b,rt); if (viewport) request.viewport = viewport;
    const valid = () => N.app.current === session && session.document.blocks.includes(b) && runtime(b) === rt && rt.sequence === sequence && rt.revision === revision;
    renderStatus(rt);
    try {
      const response = await scheduler.add(() => {
        if (!valid() || controller.signal.aborted) return null;
        rt.status = '正在查询'; renderStatus(rt);
        return N.request('POST','/api/note-query/execute',request,controller.signal);
      });
      if (!valid() || !response) return;
      rt.response = response; rt.applied = snapshot; rt.stale = false;
      rt.status = response.result.rows.length ? '查询完成' : '没有符合条件的记录'; renderResult(b,rt);
    } catch (err) {
      if (!valid()) return;
      rt.status = '查询失败'; rt.error = err.message; rt.stale = !!rt.response; renderStatus(rt);
    }
  }
  function rangeControl(b) {
    const p = b.definition.params, r = p.range || (p.range = {mode:'inherit'}), box = el('div',{class:'summary-row'});
    box.appendChild(N.select('块日期范围',[{key:'inherit',label:'继承笔记日期'},{key:'fixed',label:'固定日期'},{key:'relative',label:'动态日期'},{key:'none',label:'不限制'}],r.mode || 'inherit',value => {
      p.range = value === 'fixed' ? {mode:value,start:N.app.today,end_exclusive:C.shift(N.app.today,1)} : value === 'relative' ? {mode:value,preset:'this_week'} : {mode:value}; changed(b,true);
    }));
    if (r.mode === 'fixed') box.append(N.textInput('块开始日期',r.start,v => {r.start=v;changed(b);},'date'),N.textInput('块结束日期（含）',r.end_exclusive ? C.shift(r.end_exclusive,-1) : '',v => {r.end_exclusive=v ? C.shift(v,1) : '';changed(b);},'date'));
    if (r.mode === 'relative') box.appendChild(N.select('块动态范围',[{key:'today',label:'今天'},{key:'this_week',label:'本周'},{key:'this_month',label:'本月'}],r.preset,v => {r.preset=v;changed(b);}));
    return box;
  }
  function tagControl(b) {
    const p=b.definition.params, tag=p.tag_ids || (p.tag_ids={mode:'inherit'}),box=el('div',{class:'summary-section'});
    box.appendChild(N.select('块标签来源',[{key:'inherit',label:'继承笔记标签'},{key:'explicit',label:'独立选择（空为不筛选）'}],tag.mode || 'inherit',v => {p.tag_ids={mode:v,values:[]};changed(b,true);}));
    if (tag.mode === 'explicit') {
      const choices=el('div',{class:'note-tag-choices'});
      for (const t of state.tags) {
        const input=el('input',{type:'checkbox',onchange:() => {const values=tag.values || [];tag.values=input.checked?[...values,t.id]:values.filter(id=>id!==t.id);changed(b);}});
        input.checked=(tag.values || []).includes(t.id);choices.appendChild(el('label',{},input,t.name));
      }
      box.appendChild(choices);
    }
    return box;
  }
  function sourceFields(b) {
    if (b.definition?.kind !== 'custom') return [];
    const q=b.definition.query,source=N.app.catalog.sources.find(s=>s.key===q.source);
    return (source?.fields || []).filter(f=>!f.key.includes('.') || q.joins.some(j=>j.relation===f.key.split('.')[0]));
  }
  function panel(rt, name, label) {
    const section=el('section',{'data-editor-section':name,class:'query-tool-panel'},el('h4',{},label));
    rt.configHost.appendChild(section);return section;
  }
  function applySection(rt) {
    rt.configHost.querySelectorAll('[data-editor-section]').forEach(n=>{
      const section=n.dataset.editorSection;
      const selected=rt.section==='group'&&rt.block.query_mode==='aggregate'?(rt.block.definition?.kind==='custom'?'aggregate':'filter'):rt.section;
      n.classList.toggle('hidden',selected !== 'all' && section !== selected && !(selected === 'fields' && section === 'page'));
    });
  }
  function showPanel(rt, section) {rt.section=section;rt.details.open=true;renderConfig(rt.block,rt);}
  function renderStats(b, rt) {
    const section=panel(rt,'stats','列底统计 · 全部筛选结果');
    if (b.query_mode === 'aggregate') {section.appendChild(emptyHint('聚合报表通过返回字段配置聚合；列底统计仅用于明细表格'));return;}
    const available=columns(b), metrics=b.view.options.summaries || (b.view.options.summaries=[]);
    section.appendChild(el('p',{class:'muted'},'保留明细，统计覆盖全部匹配行；NULL 和空文本为空，0 和 false 不为空。'));
    for (const m of metrics) {
      const options=[{key:'',label:'不指定字段（总行数）'},...available.map(c=>({key:c.key,label:c.label || c.key}))];
      const col=available.find(c=>c.key===m.field);
      const ops=col?.summary_ops || ['count_all'];
      section.appendChild(el('div',{class:'summary-row'},
        N.select('统计字段',options,m.field || '',v=>{m.field=v;if(!v)m.op='count_all';else if(!columns(b).find(c=>c.key===v)?.summary_ops?.includes(m.op))m.op='count_values';changed(b,true);}),
        N.select('统计操作',ops.map(key=>({key,label:metricLabels[key]})),m.op,v=>{m.op=v;changed(b);}),
        N.button('移除统计',()=>{metrics.splice(metrics.indexOf(m),1);changed(b,true);})
      ));
    }
    section.appendChild(N.button('＋ 统计指标',()=>{metrics.push({id:C.uuid(),op:'count_all'});changed(b,true);},metrics.length>=10));
  }
  function renderGroup(b, rt) {
    const section=panel(rt,'group','展示分组 · 保留明细');
    if (b.query_mode === 'aggregate') {section.appendChild(emptyHint('请在聚合报表的字段配置中使用聚合分组'));return;}
    const current=columns(b), group=b.view.options.group;
    const options=[{key:'',label:'不分组'},...current.map(c=>({key:c.key,label:c.label || c.key}))];
    for(const f of sourceFields(b))if(!current.some(c=>c.key===f.key))options.push({key:'add:'+f.key,label:'加入返回字段并分组：'+f.label});
    section.appendChild(N.select('展示分组字段',options,group?.field || '',v=>{
      if(!v)delete b.view.options.group;
      else {
        if(v.startsWith('add:')) {
          if(!confirm('该分组字段需要加入返回字段，可在显示列中隐藏。继续？'))return;
          v=v.slice(4);const q=b.definition.query,source=N.app.catalog.sources.find(s=>s.key===q.source);
          if(!q.select.length)q.select=source.defaults.map(field=>({field}));
          if(q.select.length>=20){N.showError(new Error('返回字段最多 20 个'));return;}
          q.select.push({field:v});
        }
        b.view.options.group={field:v,direction:group?.direction || 'asc'};
      }
      changed(b,true);
    }));
    if(group)section.appendChild(N.select('分组顺序',[{key:'asc',label:'升序'},{key:'desc',label:'降序'}],group.direction,v=>{group.direction=v;changed(b);}));
    section.appendChild(el('p',{class:'muted'},'分组键优先排序，组内采用本块排序。组头显示完整组数量，跨页时标注续接。'));
  }
  function renderMapping(b, rt) {
    const section=panel(rt,'fields','展示字段与映射'), available=columns(b);
    const selected=b.view.options.columns || available.map(c=>c.key);
    const choices=el('div',{class:'note-column-options'});
    for (const c of available) {
      const check=el('input',{type:'checkbox','aria-label':'显示列 '+c.key,onchange:()=>{
        const list=b.view.options.columns || available.map(c=>c.key);
        b.view.options.columns=check.checked?[...list,c.key]:list.filter(k=>k!==c.key);N.changed();renderResult(b,rt);
      }});check.checked=selected.includes(c.key);choices.appendChild(el('label',{},check,c.label || c.key));
    }
    section.append(choices,N.textInput('显示列顺序（字段 key，逗号分隔）',selected.join(','),v=>{b.view.options.columns=v.split(',').map(x=>x.trim()).filter(Boolean);N.changed();renderResult(b,rt);}));
    const mapping=el('details',{},el('summary',{},'日历／时间轴字段映射'));
    for (const [key,label] of [['title','标题'],['date','日历日期'],['start','开始时间'],['end','结束时间'],['id','记录 ID']]) mapping.appendChild(field(label,N.select('映射'+label,[{key:'',label:'不指定'},...available.map(c=>({key:c.key,label:c.label || c.key}))],b.view.mapping[key] || '',value=>{b.view.mapping[key]=value;changed(b);})));section.appendChild(mapping);
  }
  function resetDefinition(b, definition, view) {
    const rt=runtime(b);b.definition=definition;b.query_mode=C.mode(definition);b.view=view || {type:'table',mapping:{},options:{}};
    rt.anchor=null;changed(b,true);renderViewBar(b,rt);
  }
  function renderConfig(b, rt) {
    if(!rt)return;
    rt.configHost.replaceChildren();
    const sourcePanel=panel(rt,'source','数据源与模板');
    const kind=b.definition?.kind === 'custom' ? 'custom' : b.definition?.name || '';
    sourcePanel.appendChild(N.select('查询函数',[{key:'',label:'选择数据源／模板'},{key:'custom',label:'通用查询'},...N.app.functions.map(f=>({key:f.name,label:f.label}))],kind,value=>{
      if(!confirm('切换数据源／模板会重置本块的条件、返回字段、分组、统计和映射。要保留原配置请先复制块。继续？')){renderConfig(b,rt);return;}
      if(value==='custom')resetDefinition(b,{kind:'custom',query:newSummaryQuery('todos')});
      else if(value){const fresh=C.queryBlock(value,N.app.functions.find(f=>f.name===value).label);resetDefinition(b,fresh.definition,fresh.view);}
      else resetDefinition(b,null);
    }));
    if(!b.definition){applySection(rt);return;}
    if(b.definition.kind==='function') {
      const f=N.app.functions.find(f=>f.name===b.definition.name), section=panel(rt,'filter','模板筛选');
      if(f){section.appendChild(el('p',{class:'muted'},f.description));const p=b.definition.params || (b.definition.params={});
        for(const param of f.parameters){const box=el('div',{class:'field'},el('label',{},param.label));
          if(param.type==='range')box.appendChild(rangeControl(b));
          else if(param.type==='tags')box.appendChild(tagControl(b));
          else if(param.type==='enum'){const defaults={date_field:'created_at',tag_rule:'any',status:'all',group_by:'total'};box.appendChild(N.select(param.label,param.options.map(key=>({key,label:labels[key] || key})),p[param.key] || defaults[param.key],v=>{p[param.key]=v;changed(b);}));}
          else if(param.type==='boolean'){const check=el('input',{type:'checkbox','aria-label':param.label,onchange:()=>{p[param.key]=check.checked;changed(b);}});check.checked=!!p[param.key];box.appendChild(check);}
          else if(param.type==='ids')box.appendChild(N.textInput(param.label,(p[param.key] || []).join(', '),v=>{p[param.key]=v.trim()?v.split(',').map(x=>Number(x.trim())):[];changed(b);}));
          else box.appendChild(N.textInput(param.label,p[param.key] ?? '',v=>{if(v==='')delete p[param.key];else p[param.key]=Number(v);changed(b);},'number'));
          section.appendChild(box);
        }
      }
      sourcePanel.appendChild(N.button('改为通用查询',()=>{
        if(!confirm('业务模板可能有专用统计口径。此操作将重新配置本块通用查询，不自动转换原条件。继续？'))return;
        resetDefinition(b,{kind:'custom',query:newSummaryQuery(b.definition.name==='todos'?'todos':'time_entries')});
      }));
      const sorting=panel(rt,'sort','排序');const list=b.definition.order_by || (b.definition.order_by=[]);
      for(const item of list)sorting.appendChild(el('div',{class:'summary-row'},N.select('排序字段',columns(b).map(c=>({key:c.key,label:c.label})),item.key,v=>{item.key=v;changed(b);}),N.select('排序方向',['asc','desc'],item.direction,v=>{item.direction=v;changed(b);}),N.button('移除排序',()=>{list.splice(list.indexOf(item),1);changed(b,true);})));sorting.appendChild(N.button('＋ 排序',()=>{list.push({key:columns(b)[0]?.key || '',direction:'asc'});changed(b,true);}));
    } else {
      const controller={catalog:N.app.catalog,query:{...newSummaryQuery(b.definition.query?.source || 'todos'),...C.clone(b.definition.query || {})},filter:C.clone(b.definition.query?.where || {and:[]}),busy:false};delete controller.query.where;
      const advanced=el('div');rt.configHost.appendChild(advanced);
      function update(redraw=true) {
        const oldSource=b.definition.query?.source, q=C.clone(controller.query);
        q.where=C.clone(controller.filter);b.definition.query=q;
        if(oldSource!==q.source){b.query_mode='detail';b.view={type:b.view.type,mapping:{},options:{}};delete b.definition.search;}
        changed(b,false);if(redraw){renderAdvanced();renderViewBar(b,rt);}
      }
      function renderAdvanced() {
        advanced.replaceChildren(createSummaryEditor(controller,{
          note:true,mode:b.query_mode,change:update,render:renderAdvanced,execute:()=>{update(false);execute(b);},
          beforeSourceChange:()=>confirm('更换数据源会重置本块字段、筛选、排序、分组、统计和映射。继续？')
        }));applySection(rt);
      }
      renderAdvanced();
      const bindings=b.definition.bindings || (b.definition.bindings=[]), section=panel(rt,'filter','可选笔记上下文绑定');
      for(const binding of bindings)section.appendChild(el('div',{class:'summary-row'},N.select('绑定类型',['range','tag_ids'],binding.type,v=>{binding.type=v;changed(b);}),N.textInput('绑定字段',binding.field,v=>{binding.field=v;changed(b);}),N.button('移除绑定',()=>{bindings.splice(bindings.indexOf(binding),1);changed(b,true);})));section.appendChild(N.button('＋ 上下文绑定',()=>{bindings.push({type:'range',field:'created_at',source:'context'});changed(b,true);}));
    }
    renderGroup(b,rt);renderStats(b,rt);renderMapping(b,rt);
    const search=panel(rt,'search','当前块搜索');
    const config=b.definition.search || {text:'',fields:[]};
    search.appendChild(N.textInput('搜索关键字',config.text,v=>{b.definition.search ||= config;b.definition.search.text=v;changed(b);}));
    const searchable=b.definition.name==='time_stats'?(N.app.functions.find(f=>f.name==='time_entries')?.columns || []):columns(b);
    const fields=b.definition.kind==='custom'?sourceFields(b).filter(f=>f.type==='text'):searchable.filter(f=>f.type==='text').map(c=>({key:c.key,label:c.label}));
    for(const f of fields){const check=el('input',{type:'checkbox',onchange:()=>{b.definition.search ||= config;const chosen=config.fields || [];config.fields=check.checked?[...chosen,f.key]:chosen.filter(key=>key!==f.key);changed(b);}});check.checked=(config.fields || []).includes(f.key);search.appendChild(el('label',{},check,f.label));}
    search.appendChild(el('small',{class:'muted'},'未指定搜索字段时搜索该数据源允许的文本字段，仍与本块筛选相交。'));
    applySection(rt);
  }
  function changeMode(b, rt, value) {
    if(value===b.query_mode)return;
    if(b.definition?.kind!=='custom'){N.showError(new Error('业务模板模式固定，请使用通用查询配置聚合报表'));renderViewBar(b,rt);return;}
    if(!confirm(value==='detail'?'切回明细会清除 SQL 聚合和聚合分组，恢复原始字段。继续？':'切换聚合报表会清除明细分组与列底统计，请随后配置聚合字段。继续？')){renderViewBar(b,rt);return;}
    b.query_mode=value;
    if(value==='detail'){for(const item of b.definition.query.select)delete item.aggregate;b.definition.query.select=b.definition.query.select.filter(s=>s.field);b.definition.query.group_by=[];}
    else{delete b.view.options.group;delete b.view.options.summaries;b.view.type='table';}
    rt.section='fields';changed(b,true);renderViewBar(b,rt);
  }
  function renderViewBar(b, rt) {
    rt.viewHost.replaceChildren();
    const mode=N.select('查询模式',[{key:'detail',label:'明细视图'},{key:'aggregate',label:'聚合报表'}],b.query_mode,v=>changeMode(b,rt,v));rt.viewHost.appendChild(mode);
    rt.viewHost.appendChild(N.select('结果视图',[{key:'table',label:'表格'},{key:'calendar',label:'日历'},{key:'timeline',label:'时间轴'}],b.view.type,value=>{
      if(b.query_mode==='aggregate'&&value!=='table'){N.showError(new Error('聚合报表使用表格展示'));renderViewBar(b,rt);return;}
      b.view.type=value;changed(b);renderViewBar(b,rt);execute(b);
    }));
    if(b.view.type==='table'){
      rt.viewHost.appendChild(N.select('每页记录数',['50','100','500'],String(b.definition?.query?.limit || rt.limit),v=>{rt.limit=Number(v);if(b.definition?.kind==='custom')b.definition.query.limit=rt.limit;changed(b);execute(b);}));return;
    }
    if(b.view.type==='timeline')rt.viewHost.appendChild(N.select('时间轴刻度',[{key:'day',label:'日视图'},{key:'week',label:'周视图'}],b.view.options.scale || 'week',v=>{b.view.options.scale=v;changed(b);execute(b);}));
    const move=n=>{visibleWindow(b,rt);let anchor=rt.anchor || rt.displayDate;if(b.view.type==='calendar'){const d=C.parseDay(anchor);d.setDate(1);d.setMonth(d.getMonth()+n);rt.anchor=C.day(d);}else rt.anchor=C.shift(anchor,n*(b.view.options.scale==='day'?1:7));execute(b);};
    rt.viewHost.append(N.button('上一期',()=>move(-1)),N.button('今天',()=>{rt.anchor=N.app.today;execute(b);}),N.button('下一期',()=>move(1)));
  }
  function mount(b) {
    const host=el('div',{class:'query-block'});
    b.query_mode ||= C.mode(b.definition);b.view ||= {type:'table',mapping:{},options:{}};b.view.mapping ||= {};b.view.options ||= {};
    const rt={host,block:b,collapsed:new Set(),revision:0,sequence:0,response:null,applied:null,stale:false,status:'尚未执行',error:'',offset:0,limit:50,anchor:null,section:'all'};
    rt.preview=new C.Preview(()=>execute(b));N.app.current.runtimes.set(b.id,rt);
    const title=N.textInput('查询块标题',b.title || '',v=>{b.title=v;N.changed();});title.classList.add('query-block-title');
    rt.viewHost=el('div',{class:'summary-row'});rt.statusHost=el('div',{class:'query-block-status',role:'status'});rt.resultHost=el('div',{class:'query-block-result'});
    const tools=el('div',{class:'query-toolbar','aria-label':'当前块查询工具'});
    for(const [section,label] of [['source','数据源'],['fields','字段'],['filter','筛选'],['sort','排序'],['group','分组'],['stats','统计'],['search','搜索']])tools.appendChild(N.button(label,()=>showPanel(rt,section)));
    rt.details=el('details',{class:'query-block-config'});rt.details.open=!b.definition;rt.configHost=el('div');rt.details.append(el('summary',{},'配置查询'),rt.configHost);
    host.append(title,rt.viewHost,tools,rt.details,N.button('应用并刷新',()=>execute(b)),rt.statusHost,rt.resultHost);
    host.addEventListener('compositionstart',()=>rt.preview.pause());host.addEventListener('compositionend',()=>rt.preview.resume());
    renderConfig(b,rt);renderViewBar(b,rt);renderResult(b,rt);
    if(b.definition){rt.observer=new IntersectionObserver(entries=>{if(!rt.started&&entries.some(e=>e.isIntersecting)){rt.observer.disconnect();execute(b);}},{rootMargin:'150px'});rt.observer.observe(host);}
    return host;
  }
  function refreshAll(){for(const b of N.app.current?.document.blocks || [])if(b.type==='query'&&b.definition)execute(b);}
  return {mount,invalidate,execute,refreshAll,visibleWindow};
})();
