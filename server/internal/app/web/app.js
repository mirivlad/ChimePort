'use strict';
const state = {me: null, channels: [], sources: [], messages: [], cursor: '', hasMore: false, stream: null};
const $ = (id) => document.getElementById(id);
const el = (tag, text, cls) => {const e=document.createElement(tag);if(text!==undefined)e.textContent=String(text);if(cls)e.className=cls;return e;};
function notice(message){$('status').textContent=message;}
async function api(path, opts={}) {
  const headers = {...opts.headers};
  if (opts.body && !(opts.body instanceof FormData)) headers['Content-Type']='application/json';
  const response = await fetch('/api/v1'+path,{...opts,headers,credentials:'same-origin'});
  const data = await response.json().catch(()=>({}));
  if(!response.ok) throw Error(data.error?.message || `HTTP ${response.status}`);
  return data;
}
const payload=(form)=>Object.fromEntries(new FormData(form));
function setAuth(user){
  state.me=user;$('login-panel').hidden=!!user;$('app-panel').hidden=!user;$('logout').hidden=!user;
  $('identity').textContent=user?`${user.username} · ${user.role}`:'';
  $('admin-tab').hidden=(!user||user.role!=='admin');
}
async function refreshAll(){
  const [channels,sources,messages] = await Promise.all([api('/channels'),api('/sources'),api('/messages?limit=200')]);
  state.channels=channels.items;state.sources=sources.items;state.messages=messages.items;
  state.cursor=messages.next_cursor;state.hasMore=messages.has_more;
  renderChannels();renderSources();renderMessages();
  if(state.me.role==='admin') renderUsers();
}
function showTab(id){
  document.querySelectorAll('nav .tab').forEach(b=>b.classList.toggle('active',b.dataset.tab===id));
  document.querySelectorAll('.view').forEach(v=>v.hidden=v.id!==id);
}
function renderChannels(){
  const list=$('channel-list');list.replaceChildren();const filter=$('filter-channel'),selected=filter.value;
  filter.replaceChildren(new Option('Все каналы',''));
  for(const c of state.channels){
    filter.add(new Option(`${c.name} (${c.slug})`,c.slug));
    const row=el('div',undefined,'row-item');const info=el('div');info.append(el('strong',c.name),el('p',c.slug+' · '+(c.description||'Без описания')));row.append(info);list.append(row);
  }
  filter.value=selected;
  if(!state.channels.length)list.append(el('p','Каналов пока нет. Создайте первый канал.','hint'));
}
function renderSources(){
  const list=$('source-list');list.replaceChildren();
  for(const src of state.sources){
    const row=el('div',undefined,'row-item'),info=el('div'),buttons=el('div',undefined,'actions');
    info.append(el('strong',src.name),el('p',src.description||`Источник #${src.id}`));
    const tokens=el('button','Токены','secondary'),create=el('button','Новый токен');
    tokens.addEventListener('click',()=>showTokens(src));create.addEventListener('click',()=>createToken(src));
    buttons.append(tokens,create);row.append(info,buttons);list.append(row);
  }
  if(!state.sources.length)list.append(el('p','Источников пока нет.','hint'));
}
async function showTokens(src){
  try{
    const data=await api(`/sources/${src.id}/tokens`);
    const host=el('div');host.append(el('h3','Токены источника «'+src.name+'»'));
    for(const token of data.items){
      const row=el('div',undefined,'row-item');const info=el('div');
      info.append(el('strong',token.label+' ('+token.prefix+'…)'),el('p',(token.description||'')+(token.revoked_at?' · Отозван':'')));
      row.append(info);
      if(!token.revoked_at){
        const actions=el('div',undefined,'actions');
        const reveal=el('button','Показать','secondary');
        const copy=el('button','Копировать','secondary');
        let visible=false;let secret='';
        const value=el('code','••••••••••••');
        value.classList.add('token-secret');
        reveal.addEventListener('click',async()=>{
          if(visible){visible=false;secret='';value.textContent='••••••••••••';reveal.textContent='Показать';return;}
          try{const data=await api(`/sources/${src.id}/tokens/${token.id}/secret`);secret=data.token;visible=true;value.textContent=secret;reveal.textContent='Скрыть';}
          catch(err){notice(err.message);}
        });
        copy.addEventListener('click',async()=>{
          try{const data=await api(`/sources/${src.id}/tokens/${token.id}/secret`);await navigator.clipboard.writeText(data.token);notice('Токен скопирован');}
          catch(err){notice(err.message);}
        });
        info.append(value);actions.append(reveal,copy);row.append(actions);
        const button=el('button','Отозвать','secondary');button.addEventListener('click',async()=>{
        if(!confirm('Отозвать токен? Отправка сообщений с ним прекратится.'))return;
        try {await api(`/sources/${src.id}/tokens/${token.id}`,{method:'DELETE'});notice('Токен отозван');host.remove();showTokens(src);}catch(err){notice(err.message);}
      });row.append(button);}host.append(row);
    }
    if(!data.items.length)host.append(el('p','Токенов нет.'));
    const old=$('token-details');if(old)old.remove();host.id='token-details';$('source-list').prepend(host);
  }catch(err){notice(err.message);}
}
async function createToken(src){
  if(!state.channels.length){notice('Сначала создайте хотя бы один канал.');return;}
  const label=prompt('Для чего нужен токен? Название (например, HA — двери)');if(!label)return;
  const description=prompt('Описание токена (необязательно)','')??'';
  const allowed=prompt('Разрешённые каналы через запятую:\n'+state.channels.map(c=>`${c.id} = ${c.slug}`).join('\n'),state.channels[0].id);
  if(!allowed)return;
  const channel_ids=allowed.split(',').map(x=>Number(x.trim())).filter(Number.isSafeInteger);
  try{
    const created=await api(`/sources/${src.id}/tokens`,{method:'POST',body:JSON.stringify({label,description,channel_ids})});
    const dialog=$('token-dialog-template').content.firstElementChild.cloneNode(true);
    dialog.querySelector('code').textContent=created.token;
    dialog.querySelector('.copy').addEventListener('click',()=>navigator.clipboard.writeText(created.token).then(()=>notice('Токен скопирован')));
    dialog.querySelector('.close').addEventListener('click',()=>dialog.close());
    dialog.addEventListener('close',()=>dialog.remove());document.body.append(dialog);dialog.showModal();
  }catch(err){notice(err.message);}
}
function renderMessages(){
  const list=$('messages'),filter=$('filter-channel').value;list.replaceChildren();
  const messages=[...state.messages].reverse().filter(m=>!filter||m.channel===filter);
  for(const msg of messages){
    const item=el('article',undefined,'message '+(msg.read_at?'':'unread'));
    const heading=el('div',undefined,'message-title');heading.append(el('h2',msg.title));
    if(!msg.read_at){const read=el('button','Прочитано','secondary');read.addEventListener('click',async()=>{
      try{await api(`/messages/${msg.id}/read`,{method:'POST'});msg.read_at=new Date().toISOString();renderMessages();}catch(err){notice(err.message);}
    });heading.append(read);}
    item.append(heading,el('p',msg.body),el('div',`${msg.channel} · ${msg.source} · ${msg.priority} · ${new Date(msg.created_at).toLocaleString()}`,'meta'));
    if(msg.url){const link=el('a','Открыть ссылку');link.href=msg.url;link.target='_blank';link.rel='noopener noreferrer';item.append(link);}
    list.append(item);
  }
  if(!messages.length)list.append(el('p','Сообщений пока нет.','hint'));
  $('load-more').hidden=!state.hasMore;
}
async function fetchNew(){
  if(!state.me)return;
  try{
    let after=state.cursor;
    for(let i=0;i<20;i++){
      const data=await api('/messages?limit=200&after='+encodeURIComponent(after));
      if(data.items.length){state.messages.push(...data.items);state.messages=state.messages.slice(-1000);}
      after=data.next_cursor;state.cursor=after;
      if(!data.has_more)break;
    }
    renderMessages();
  }catch(err){notice(err.message);}
}
function startStream(){
  if(state.stream)state.stream.close();
  state.stream=new EventSource('/api/v1/events');
  state.stream.addEventListener('messages_available',()=>fetchNew());
  state.stream.addEventListener('open',()=>fetchNew());
}
async function renderUsers(){
  try{const data=await api('/admin/users'),list=$('user-list');list.replaceChildren();
    for(const user of data.items){const row=el('div',undefined,'row-item');row.append(el('strong',user.username),el('span',user.role+(user.disabled?' · заблокирован':'')));list.append(row);}
  }catch(err){notice(err.message);}
}
async function connect(){
  try{const me=await api('/me');setAuth(me);await refreshAll();startStream();}catch(err){setAuth(null);}
}
$('login-form').addEventListener('submit',async(e)=>{e.preventDefault();const data=payload(e.target);
  try{await api('/auth/login',{method:'POST',body:JSON.stringify({...data,mode:'web'})});notice('');e.target.reset();await connect();}catch(err){notice(err.message);}
});
$('logout').addEventListener('click',async()=>{
  try{await api('/auth/logout',{method:'POST'});}catch(err){notice(err.message);}
  if(state.stream)state.stream.close();state.stream=null;setAuth(null);state.messages=[];
});
document.querySelectorAll('nav .tab').forEach(b=>b.addEventListener('click',()=>showTab(b.dataset.tab)));
$('refresh').addEventListener('click',async()=>{try{await refreshAll();notice('Обновлено');}catch(err){notice(err.message);}});
$('filter-channel').addEventListener('change',renderMessages);
$('load-more').addEventListener('click',fetchNew);
$('channel-form').addEventListener('submit',async(e)=>{e.preventDefault();try{await api('/channels',{method:'POST',body:JSON.stringify(payload(e.target))});e.target.reset();await refreshAll();notice('Канал создан');}catch(err){notice(err.message);}});
$('source-form').addEventListener('submit',async(e)=>{e.preventDefault();try{await api('/sources',{method:'POST',body:JSON.stringify(payload(e.target))});e.target.reset();await refreshAll();notice('Источник создан');}catch(err){notice(err.message);}});
$('user-form').addEventListener('submit',async(e)=>{e.preventDefault();try{await api('/admin/users',{method:'POST',body:JSON.stringify(payload(e.target))});e.target.reset();await renderUsers();notice('Пользователь создан');}catch(err){notice(err.message);}});
connect();
