import type { ServerMsg } from "./types";

export type ClientMsg = {
  type: string;
  id?: number;
  subscribe?: { sub_id: number; filter: string; from_seq?: number; history_n?: number };
  unsubscribe?: { sub_id: number };
  pause?: { sub_id: number };
  resume?: { sub_id: number };
  filter?: { sub_id: number; filter: string };
  add_source?: { kind: string; name?: string; args?: Record<string, unknown> };
  remove_source?: { source_id: number };
  history?: { sub_id: number; before_seq: number; limit: number };
  ping?: { nonce: number };
};

// Bus is a self-reconnecting WebSocket client. Callers register handlers and
// can call send() at any time; messages sent while disconnected are queued
// and flushed on reconnect.
export class Bus {
  private url: string;
  private ws: WebSocket | null = null;
  private nextID = 1;
  private queue: ClientMsg[] = [];
  private closed = false;
  private backoffMs = 250;

  public onmessage: (m: ServerMsg) => void = () => {};
  public onstatus: (open: boolean) => void = () => {};

  constructor(url: string) {
    this.url = url;
    this.connect();
  }

  private connect() {
    if (this.closed) return;
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.onopen = () => {
      this.backoffMs = 250;
      this.onstatus(true);
      // Flush queued messages.
      const q = this.queue;
      this.queue = [];
      for (const m of q) ws.send(JSON.stringify(m));
    };
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as ServerMsg;
        this.onmessage(msg);
      } catch (err) {
        console.error("ws parse:", err);
      }
    };
    ws.onclose = () => {
      this.onstatus(false);
      this.ws = null;
      if (!this.closed) {
        const wait = Math.min(this.backoffMs, 5000);
        setTimeout(() => this.connect(), wait);
        this.backoffMs *= 2;
      }
    };
    ws.onerror = () => ws.close();
  }

  send(m: ClientMsg): number {
    if (m.id === undefined) m.id = this.nextID++;
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(m));
    } else {
      this.queue.push(m);
    }
    return m.id;
  }

  close() {
    this.closed = true;
    this.ws?.close();
  }
}
