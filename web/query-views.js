'use strict';
const QueryViews = (() => {
  const C=NoteCore;
  function button(label,fn,disabled=false){const b=el('button',{type:'button',class:'btn btn-small',onclick:fn},label);b.disabled=disabled;return b;}
  function detail(piece){const box=el('div',{class:'stack'},el('h4',{},piece.title));for(const [key,value] of Object.entries(piece.row))box.appendChild(el('div',{class:'note-detail-row'},el('strong',{},key),el('span',{},value==null?'∅':String(value))));openModal('记录详情',box);}
  const metricLabels={count_all:'总行数',count_values:'已填写',count_not_empty:'非空',count_empty:'未填写',count_distinct:'去重数',percent_empty:'未填写比例',percent_not_empty:'已填写比例',sum:'合计',avg:'平均',min:'最小',max:'最大'};
  function metricText(m) {
    const value=m.value==null?'∅':typeof m.value==='number'&&!Number.isInteger(m.value)?m.value.toFixed(2):String(m.value);
    return `${metricLabels[m.op]||m.op} ${value}${m.op.startsWith('percent_')&&m.value!=null?'%':''}`;
  }
  function table(b,response,actions){
    const result=response.result,host=el('div',{class:'note-table-view'}),order=b.view.options.columns;
    const columns=order?order.map(key=>result.columns.find(c=>c.key===key)).filter(Boolean):result.columns;
    host.appendChild(el('p',{class:'muted'},`本页 ${result.row_count} 行 · 共 ${result.total_count} 行`));
    const dataRow=row=>{
      const tr=el('tr');
      for(const c of columns){const value=row[c.key],td=el('td',{class:c.type==='number'?'summary-number':''});
        if(value==null)td.appendChild(document.createTextNode('∅'));
        else if(String(value).length>140)td.appendChild(el('details',{},el('summary',{},String(value).slice(0,100)+'…'),el('p',{class:'note-long-text'},String(value))));
        else td.appendChild(document.createTextNode(String(value)));
        tr.appendChild(td);
      }return tr;
    };
    if(!result.rows.length)host.appendChild(emptyHint('没有符合条件的记录'));
    else if(!columns.length)host.appendChild(emptyHint('未选择显示列，请在字段面板中设置'));
    else{
      const table=el('table',{class:'summary-table'}),head=el('tr');
      for(const c of columns){const current=actions.sort.find(o=>o.key===c.key),label=(c.label||c.key)+(current?(current.direction==='asc'?' ↑':' ↓'):'');
        const sort=button(label,()=>actions.onSort([{key:c.key,direction:current?.direction==='asc'?'desc':'asc'}]),actions.stale);sort.dataset.queryPage='true';sort.dataset.unavailable='false';head.appendChild(el('th',{scope:'col'},sort));
      }
      table.appendChild(el('thead',{},head));
      if(response.groups?.length){
        for(const group of response.groups){
          const body=el('tbody',{'data-group-key':JSON.stringify(group.key)}),rows=group.page_row_indexes.map(i=>dataRow(result.rows[i]));
          const key=JSON.stringify(group.key),collapsed=actions.collapsed?.has(key);
          const title=`${group.label} · 本页 ${rows.length} 条 / 本组 ${group.row_count} 条${group.has_previous_rows?' · 续上页':''}${group.has_next_rows?' · 下页继续':''}`;
          const toggle=button((collapsed?'▸ ':'▾ ')+title,()=>{const hidden=actions.collapsed.has(key);if(hidden)actions.collapsed.delete(key);else actions.collapsed.add(key);rows.forEach(row=>row.classList.toggle('hidden',!hidden));toggle.textContent=(hidden?'▾ ':'▸ ')+title;toggle.setAttribute('aria-expanded',String(hidden));});toggle.setAttribute('aria-expanded',String(!collapsed));
          const cell=el('td',{colspan:columns.length},toggle);
          if(group.summaries?.length)cell.appendChild(el('small',{class:'query-group-stats'},'本组全部记录：'+group.summaries.map(m=>(m.field?m.field+' ':'')+metricText(m)).join(' · ')));
          body.appendChild(el('tr',{class:'query-group-header'},cell));rows.forEach(row=>{row.classList.toggle('hidden',!!collapsed);body.appendChild(row);});table.appendChild(body);
        }
      }else table.appendChild(el('tbody',{},...result.rows.map(dataRow)));
      if(response.summaries){
        const tr=el('tr');columns.forEach((c,i)=>{const metrics=response.summaries.items.filter(m=>m.field===c.key||(!m.field&&i===0));tr.appendChild(el('td',{class:'query-stat-cell'},...metrics.map(m=>el('div',{'data-metric-id':m.id},metricText(m)))));});table.appendChild(el('tfoot',{},tr));
      }
      host.appendChild(el('div',{class:'summary-table-scroll',tabindex:'0','aria-label':'查询结果表格'},table));
    }
    if(response.summaries){const box=el('div',{class:'query-total-stats','aria-label':'全部筛选结果统计'},el('strong',{},`全部筛选结果 · ${response.summaries.matched_row_count} 条`));
      for(const m of response.summaries.items)box.appendChild(el('span',{'data-metric-id':m.id},(m.field?m.field+'：':'')+metricText(m)));host.appendChild(box);
    }
    const previous=button('上一页',()=>actions.onPage(Math.max(0,actions.offset-actions.limit)),actions.offset===0||actions.stale),next=button('下一页',()=>actions.onPage(result.next_offset),result.next_offset==null||result.next_offset>100000||actions.stale);
    previous.dataset.queryPage=next.dataset.queryPage='true';previous.dataset.unavailable=String(actions.offset===0);next.dataset.unavailable=String(result.next_offset==null||result.next_offset>100000);
    host.appendChild(el('div',{class:'summary-row'},previous,el('span',{},`第 ${Math.floor(actions.offset/actions.limit)+1} 页`),next));return host;
  }
  function range(meta){let start=meta.viewport.start,end=meta.viewport.end_exclusive;const b=meta.effective_range;if(b){if(b.start>start)start=b.start;if(b.end_exclusive<end)end=b.end_exclusive;}return {start,end_exclusive:end};}
  function calendar(b,response){
    const host=el('div',{class:'note-calendar-view'}),meta=response.meta;const visible=meta.viewport;if(!visible)return emptyHint('缺少日历日期范围');
    const pieces=C.pieces(response.result.rows,b.view.mapping,meta.as_of,range(meta));const focus=C.shift(visible.start,14);host.appendChild(el('h4',{},focus.slice(0,7)));
    if(pieces.missing)host.appendChild(el('p',{class:'muted'},`${pieces.missing} 条记录无有效日期，未放入日历`));
    const grid=el('div',{class:'note-calendar',role:'group','aria-label':'月历'});for(const name of ['一','二','三','四','五','六','日'])grid.appendChild(el('div',{class:'note-calendar-heading'},name));
    const byDay=new Map();for(const item of pieces.items){if(!byDay.has(item.date))byDay.set(item.date,[]);byDay.get(item.date).push(item);}
    for(let date=visible.start;date<visible.end_exclusive;date=C.shift(date,1)){
      const box=el('div',{class:'note-calendar-day'+(date.slice(0,7)!==focus.slice(0,7)?' note-calendar-other':''),'data-date':date},el('time',{datetime:date},String(Number(date.slice(8)))));const items=byDay.get(date)||[];
      for(const item of items.slice(0,3)){const event=button(`${item.from!=='00:00:00'?item.from.slice(0,5)+' ':''}${item.title}`,()=>detail(item));event.classList.add('note-calendar-event');event.style.borderLeftColor=todoColor(item.row.todo_id||item.id);event.title=item.title;box.appendChild(event);}
      if(items.length>3){const more=el('details',{},el('summary',{},`另 ${items.length-3} 条`));for(const item of items.slice(3))more.appendChild(button(item.title,()=>detail(item)));box.appendChild(more);}grid.appendChild(box);
    }
    host.appendChild(grid);return host;
  }
  function timeline(b,response){
    const host=el('div',{class:'note-timeline-view'}),meta=response.meta,visible=meta.viewport;if(!visible)return emptyHint('缺少时间轴日期范围');
    host.appendChild(el('h4',{},`${visible.start} — ${C.shift(visible.end_exclusive,-1)}`));const pieces=C.pieces(response.result.rows,b.view.mapping,meta.as_of,range(meta));
    if(pieces.missing)host.appendChild(el('p',{class:'muted'},`${pieces.missing} 条记录无有效时间，未放入时间轴`));
    const days=[];for(let d=visible.start;d<visible.end_exclusive;d=C.shift(d,1))days.push(d);
    const scroll=el('div',{class:'note-timeline-scroll',tabindex:'0','aria-label':'按日时间轴'});const grid=el('div',{class:'note-timeline-grid'});grid.style.gridTemplateColumns=`44px repeat(${days.length},minmax(100px,1fr))`;
    grid.appendChild(el('div'));for(const d of days)grid.appendChild(el('div',{class:'note-timeline-heading'},d.slice(5)));
    const axis=el('div',{class:'note-time-axis'});for(let hour=0;hour<=24;hour++){const tick=el('span',{},`${String(hour).padStart(2,'0')}:00`);tick.style.top=`${hour*40}px`;axis.appendChild(tick);}grid.appendChild(axis);
    for(const d of days){const col=el('div',{class:'note-time-day','data-date':d});for(const item of C.lanes(pieces.items.filter(i=>i.date===d))){const n=button(`${item.from.slice(0,5)}–${item.to.slice(0,5)} ${item.title}`,()=>detail(item));n.classList.add('note-time-event');n.style.top=`${item.start/60*40}px`;n.style.height=`${Math.max((item.end-item.start)/60*40,18)}px`;n.style.left=`${item.lane/item.lanes*100}%`;n.style.width=`${100/item.lanes}%`;n.style.backgroundColor=todoColor(item.row.todo_id||item.id);n.title=`${item.title}\n${item.date} ${item.from}–${item.to}`;col.appendChild(n);}grid.appendChild(col);}
    scroll.appendChild(grid);host.appendChild(scroll);return host;
  }
  function render(b,response,actions){
    const host=el('div');const meta=response.meta;for(const warning of meta.warnings||[])host.appendChild(el('p',{class:'summary-error'},`${warning.message}（${warning.count}）`));
    const type=b.view.type;if(type==='table')host.appendChild(table(b,response,actions));else if(!meta.complete)host.appendChild(emptyHint('结果不完整，请缩小日期范围或改用表格'));else if(type==='calendar')host.appendChild(calendar(b,response));else host.appendChild(timeline(b,response));
    if(type!=='table'&&meta.effective_range&&meta.viewport&&(meta.effective_range.end_exclusive<=meta.viewport.start||meta.effective_range.start>=meta.viewport.end_exclusive))host.appendChild(emptyHint('当前可见日期在查询业务范围之外'));
    return host;
  }
  return {render};
})();
