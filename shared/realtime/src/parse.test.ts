import { describe, expect, it } from 'vitest';
import { createSseParser, type SseMessage } from './parse';

function run(chunks: string[]) {
  const messages: SseMessage[] = [];
  const ids: string[] = [];
  const retries: number[] = [];
  const comments: string[] = [];
  const p = createSseParser({
    onMessage: (m) => messages.push(m),
    onId: (i) => ids.push(i),
    onRetry: (r) => retries.push(r),
    onComment: (c) => comments.push(c),
  });
  for (const c of chunks) p.feed(c);
  return { messages, ids, retries, comments };
}

describe('createSseParser', () => {
  it('parses a simple event', () => {
    const r = run(['event: order.created\nid: 5\ndata: {"a":1}\n\n']);
    expect(r.messages).toEqual([{ event: 'order.created', data: '{"a":1}', id: '5' }]);
  });

  it('handles chunk boundaries anywhere', () => {
    const text = 'retry: 3000\n\nevent: x\nid: 9\ndata: hello\n\n';
    const r = run(text.split(''));
    expect(r.retries).toEqual([3000]);
    expect(r.messages).toEqual([{ event: 'x', data: 'hello', id: '9' }]);
  });

  it('handles CRLF, CR and LF, including CRLF split across chunks', () => {
    const r = run(['event: a\r\ndata: 1\r\n\r\n', 'event: b\rdata: 2\r\r', 'event: c\r', '\ndata: 3\r', '\n\r', '\n']);
    expect(r.messages.map((m) => [m.event, m.data])).toEqual([
      ['a', '1'],
      ['b', '2'],
      ['c', '3'],
    ]);
  });

  it('records an id-only frame without dispatching', () => {
    const r = run([': hb\n\nid: 42\n\n']);
    expect(r.messages).toEqual([]);
    expect(r.ids).toEqual(['42']);
    expect(r.comments).toEqual([' hb']);
  });

  it('joins multi-line data and defaults the event name', () => {
    const r = run(['data: a\ndata: b\ndata:\n\n']);
    expect(r.messages).toEqual([{ event: 'message', data: 'a\nb\n', id: '' }]);
  });

  it('ignores ids containing NUL and bad retry values', () => {
    const r = run(['id: a\0b\nretry: soon\ndata: x\n\n']);
    expect(r.ids).toEqual([]);
    expect(r.retries).toEqual([]);
    expect(r.messages[0]?.id).toBe('');
  });

  it('keeps the last id for later events and ignores unknown fields', () => {
    const r = run(['id: 1\nfoo: bar\ndata: a\n\ndata: b\n\n']);
    expect(r.messages.map((m) => m.id)).toEqual(['1', '1']);
  });

  it('does not strip more than one leading space', () => {
    const r = run(['data:  two\n\n']);
    expect(r.messages[0]?.data).toBe(' two');
  });
});
