/** One dispatched SSE event. `id` is the last event id seen so far (may come from an earlier frame). */
export interface SseMessage {
  event: string;
  data: string;
  id: string;
}

export interface SseParserHandlers {
  onMessage(m: SseMessage): void;
  /** Every accepted `id:` line, including id-only frames that dispatch nothing. */
  onId?(id: string): void;
  onRetry?(ms: number): void;
  onComment?(text: string): void;
}

export interface SseParser {
  /** Feed decoded text. Chunk boundaries may fall anywhere, including inside CRLF. */
  feed(text: string): void;
}

/**
 * Incremental parser for the text/event-stream format (WHATWG HTML, "Server-sent events").
 * Pure: no timers, no I/O. Handles LF, CR and CRLF, comments, multi-line data, `retry`, ids
 * containing NUL (ignored) and id lines that are never followed by data.
 */
export function createSseParser(h: SseParserHandlers): SseParser {
  let buf = '';
  let data: string[] = [];
  let hasData = false;
  let eventType = '';
  let lastId = '';

  function line(l: string): void {
    if (l === '') {
      if (hasData) {
        h.onMessage({ event: eventType || 'message', data: data.join('\n'), id: lastId });
      }
      data = [];
      hasData = false;
      eventType = '';
      return;
    }
    if (l.startsWith(':')) {
      h.onComment?.(l.slice(1));
      return;
    }
    const i = l.indexOf(':');
    const field = i === -1 ? l : l.slice(0, i);
    let value = i === -1 ? '' : l.slice(i + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    switch (field) {
      case 'event':
        eventType = value;
        break;
      case 'data':
        data.push(value);
        hasData = true;
        break;
      case 'id':
        if (!value.includes('\0')) {
          lastId = value;
          h.onId?.(value);
        }
        break;
      case 'retry':
        if (/^[0-9]+$/.test(value)) h.onRetry?.(Number(value));
        break;
      default:
        break;
    }
  }

  return {
    feed(text) {
      buf += text;
      let start = 0;
      for (let i = 0; i < buf.length; i++) {
        const c = buf.charCodeAt(i);
        if (c === 10) {
          line(buf.slice(start, i));
          start = i + 1;
        } else if (c === 13) {
          // A trailing CR may be the first half of CRLF: wait for the next chunk.
          if (i === buf.length - 1) break;
          line(buf.slice(start, i));
          if (buf.charCodeAt(i + 1) === 10) i++;
          start = i + 1;
        }
      }
      buf = buf.slice(start);
    },
  };
}
