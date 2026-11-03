'use strict';
const channelSelect = document.getElementById('example-channel');
const examplesList = document.getElementById('examples-list');
const chosenChannel = new URLSearchParams(location.search).get('channel') || '';
if (chosenChannel) {
  channelSelect.replaceChildren(new Option(chosenChannel, chosenChannel));
}

function publishingURL() {
  const url = new URL('/api/v1/messages', location.origin);
  url.searchParams.set('channel', channelSelect.value);
  return url.toString();
}

function examples() {
  const url = publishingURL();
  const quoted = JSON.stringify(url);
  const payload = '{"title":"Проверка","body":"Привет из ChimePort!","priority":"normal"}';
  const goQuote = String.fromCharCode(96);
  return [
    {
      title: 'Терминал / curl',
      caption: 'Самый простой вариант для Linux, macOS и скриптов Bash',
      code: [
        "export CHIMEPORT_TOKEN='cp_t_ВАШ_ТОКЕН'",
        'curl --fail-with-body -sS -X POST ' + quoted +
          ' -H "Authorization: Bearer $CHIMEPORT_TOKEN" -H "Content-Type: application/json" -d ' + "'" + payload + "'",
        'unset CHIMEPORT_TOKEN'
      ].join('\n')
    },
    {
      title: 'Python',
      caption: 'Только стандартная библиотека, без дополнительных модулей',
      code: [
        'import os, json, urllib.request',
        '',
        'url = ' + quoted,
        'data = json.dumps({"title": "Проверка", "body": "Из Python", "priority": "normal"}).encode("utf-8")',
        'req = urllib.request.Request(url, data=data, method="POST", headers={',
        '    "Authorization": "Bearer " + os.environ["CHIMEPORT_TOKEN"],',
        '    "Content-Type": "application/json",',
        '})',
        'with urllib.request.urlopen(req, timeout=15) as response:',
        '    print(response.read().decode("utf-8"))'
      ].join('\n')
    },
    {
      title: 'JavaScript / Node.js',
      caption: 'Node.js 18+ с встроенным fetch',
      code: [
        'const res = await fetch(' + quoted + ', {',
        '  method: "POST",',
        '  headers: { Authorization: "Bearer " + process.env.CHIMEPORT_TOKEN, "Content-Type": "application/json" },',
        '  body: JSON.stringify({ title: "Проверка", body: "Из Node.js", priority: "normal" })',
        '});',
        'if (!res.ok) throw new Error(await res.text());',
        'console.log(await res.json());'
      ].join('\n')
    },
    {
      title: 'Go',
      caption: 'Стандартная библиотека net/http',
      code: [
        'package main',
        'import ("bytes"; "fmt"; "net/http"; "os")',
        'func main() {',
        '  body := []byte(' + goQuote + payload + goQuote + ')',
        '  req, err := http.NewRequest("POST", ' + quoted + ', bytes.NewReader(body))',
        '  if err != nil { panic(err) }',
        '  req.Header.Set("Authorization", "Bearer " + os.Getenv("CHIMEPORT_TOKEN"))',
        '  req.Header.Set("Content-Type", "application/json")',
        '  resp, err := http.DefaultClient.Do(req)',
        '  if err != nil { panic(err) }',
        '  defer resp.Body.Close()',
        '  fmt.Println("HTTP status:", resp.Status)',
        '}'
      ].join('\n')
    },
    {
      title: 'PowerShell',
      caption: 'Windows или PowerShell 7 под Linux',
      code: [
        '$headers = @{ Authorization = "Bearer $env:CHIMEPORT_TOKEN" }',
        '$body = @{ title = "Проверка"; body = "Из PowerShell"; priority = "normal" } | ConvertTo-Json',
        'Invoke-RestMethod -Uri ' + quoted + ' -Method Post -Headers $headers -ContentType "application/json; charset=utf-8" -Body $body'
      ].join('\n')
    },
    {
      title: 'Home Assistant',
      caption: 'REST command с токеном в secrets.yaml',
      code: [
        '# configuration.yaml',
        'rest_command:',
        '  chimeport_test:',
        '    url: ' + quoted,
        '    method: POST',
        '    content_type: "application/json"',
        '    headers:',
        '      Authorization: !secret chimeport_authorization',
        "    payload: '" + payload + "'",
        '',
        '# secrets.yaml',
        'chimeport_authorization: "Bearer cp_t_ВАШ_ТОКЕН"',
        '',
        '# Из автоматизации вызывайте действие rest_command.chimeport_test'
      ].join('\n')
    }
  ];
}
function renderExamples() {
  examplesList.replaceChildren();
  for (const item of examples()) {
    const section = document.createElement('section');
    section.className = 'panel example-card';
    const head = document.createElement('div');
    head.className = 'section-heading';
    const heading = document.createElement('div');
    const title = document.createElement('h2');
    title.textContent = item.title;
    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = item.caption;
    heading.append(title, hint);
    const copy = document.createElement('button');
    copy.type = 'button';
    copy.className = 'secondary';
    copy.textContent = 'Копировать';
    copy.addEventListener('click', async () => {
      try { await navigator.clipboard.writeText(item.code); copy.textContent = 'Скопировано'; }
      catch { copy.textContent = 'Скопируйте текст вручную'; }
    });
    head.append(heading, copy);
    const pre = document.createElement('pre');
    const code = document.createElement('code');
    code.textContent = item.code;
    pre.append(code);
    section.append(head, pre);
    examplesList.append(section);
  }
}
async function loadChannels() {
  try {
    const response = await fetch('/api/v1/channels', { credentials: 'same-origin' });
    if (!response.ok) return;
    const data = await response.json();
    if (!Array.isArray(data.items) || !data.items.length) return;
    channelSelect.replaceChildren();
    for (const channel of data.items) {
      const option = new Option(channel.name + ' (' + channel.slug + ')', channel.slug);
      channelSelect.add(option);
    }
    if (chosenChannel && data.items.some((item) => item.slug === chosenChannel)) {
      channelSelect.value = chosenChannel;
    }
  } catch { /* Examples are also available without login. */ }
}
channelSelect.addEventListener('change', renderExamples);
loadChannels().then(renderExamples);
