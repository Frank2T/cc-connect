const http = require('http');
const fs = require('fs');
const log = 'E:/CodexTelegram/.ccsrc/core/mock-requests.jsonl';
const server = http.createServer((req, res) => {
  let body = '';
  req.on('data', (c) => (body += c));
  req.on('end', () => {
    fs.appendFileSync(log, JSON.stringify({ url: req.url, body }) + '\n');
    res.writeHead(200, { 'Content-Type': 'text/event-stream' });
    const id = 'resp_mock_1';
    const now = Math.floor(Date.now() / 1000);
    const events = [
      { type: 'response.created', response: { id, object: 'response', created_at: now, status: 'in_progress', model: 'deepseek-v4-flash', output: [] } },
      { type: 'response.output_item.added', output_index: 0, item: { id: 'rsn_1', type: 'reasoning', status: 'in_progress', summary: [], content: [] } },
      { type: 'response.reasoning_text.delta', item_id: 'rsn_1', output_index: 0, content_index: 0, delta: 'I am thinking step by step.' },
      { type: 'response.reasoning_text.done', item_id: 'rsn_1', output_index: 0, content_index: 0, text: 'I am thinking step by step.' },
      { type: 'response.output_item.done', output_index: 0, item: { id: 'rsn_1', type: 'reasoning', status: 'completed', summary: [], content: [{ type: 'reasoning_text', text: 'I am thinking step by step.' }] } },
      { type: 'response.output_item.added', output_index: 1, item: { id: 'msg_1', type: 'message', status: 'in_progress', role: 'assistant', content: [] } },
      { type: 'response.content_part.added', item_id: 'msg_1', output_index: 1, content_index: 0, part: { type: 'output_text', text: '', annotations: [] } },
      { type: 'response.output_text.delta', item_id: 'msg_1', output_index: 1, content_index: 0, delta: 'hi' },
      { type: 'response.output_text.done', item_id: 'msg_1', output_index: 1, content_index: 0, text: 'hi' },
      { type: 'response.output_item.done', output_index: 1, item: { id: 'msg_1', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'hi', annotations: [] }] } },
      { type: 'response.completed', response: { id, object: 'response', status: 'completed', model: 'deepseek-v4-flash', output: [{ id: 'rsn_1', type: 'reasoning', status: 'completed', summary: [], content: [{ type: 'reasoning_text', text: 'I am thinking step by step.' }] }, { id: 'msg_1', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'hi', annotations: [] }] }], usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15 } } },
    ];
    for (const e of events) res.write('data: ' + JSON.stringify(e) + '\n\n');
    res.end('data: [DONE]\n\n');
  });
});
server.listen(18899, '127.0.0.1');
