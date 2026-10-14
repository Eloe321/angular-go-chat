import { PLATFORM_ID } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { vi } from 'vitest';
import { CHAT_SOCKET_FACTORY, ChatService } from './chat.service';
import { ChatSocket, Message, Room } from './chat.models';

class FakeSocket implements ChatSocket {
  readyState = 0;
  onopen: ChatSocket['onopen'] = null;
  onmessage: ChatSocket['onmessage'] = null;
  onclose: ChatSocket['onclose'] = null;
  onerror: ChatSocket['onerror'] = null;
  sent: string[] = [];
  close = vi.fn(() => {
    this.readyState = 3;
  });
  send(data: string): void {
    this.sent.push(data);
  }
  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }
  event(data: unknown): void {
    this.raw(JSON.stringify(data));
  }
  raw(data: string): void {
    this.onmessage?.(new MessageEvent('message', { data }));
  }
  disconnect(): void {
    this.readyState = 3;
    this.onclose?.(new CloseEvent('close'));
  }
}
const rooms: Room[] = [
  { id: 'general', name: 'General', description: 'All things development.' },
  { id: 'angular', name: 'Angular', description: 'Components and signals.' },
  { id: 'go', name: 'Go', description: 'Goroutines and services.' },
];
const message = (id: string, roomId = 'general'): Message => ({
  id,
  roomId,
  senderId: 'guest-2',
  senderName: 'Mina',
  content: `Message ${id}`,
  createdAt: '2026-10-14T06:00:00Z',
});

describe('ChatService with browser transport', () => {
  let chat: ChatService;
  let http: HttpTestingController;
  let sockets: FakeSocket[];
  let urls: string[];
  beforeEach(() => {
    sockets = [];
    urls = [];
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        {
          provide: CHAT_SOCKET_FACTORY,
          useValue: (url: string) => {
            urls.push(url);
            const socket = new FakeSocket();
            sockets.push(socket);
            return socket;
          },
        },
      ],
    });
    chat = TestBed.inject(ChatService);
    http = TestBed.inject(HttpTestingController);
  });
  afterEach(() => {
    TestBed.resetTestingModule();
    vi.useRealTimers();
    http.verify();
  });
  function boot(): void {
    chat.initializeBrowser();
    http.expectOne('/api/rooms').flush({ rooms });
  }
  function join(): FakeSocket {
    boot();
    expect(chat.join('general', '  A & 😀  ')).toBe(true);
    return sockets[0];
  }
  function ready(socket: FakeSocket, history: Message[] = [], room = rooms[0]): void {
    socket.open();
    socket.event({
      type: 'session.ready',
      sessionId: 'me',
      room,
      messages: history,
      participants: [{ id: 'me', name: 'A & 😀' }],
    });
  }

  it('does no REST or socket work until browser initialization', () => {
    http.expectNone('/api/rooms');
    expect(sockets).toHaveLength(0);
    boot();
    expect(chat.rooms()).toEqual(rooms);
    expect(chat.roomsLoading()).toBe(false);
    chat.initializeBrowser();
    http.expectNone('/api/rooms');
  });
  it('shows a room-loading failure and lets the user retry', () => {
    chat.initializeBrowser();
    expect(chat.roomsLoading()).toBe(true);
    http.expectOne('/api/rooms').flush('offline', { status: 503, statusText: 'Unavailable' });
    expect(chat.roomError()).toContain('Retry');
    expect(chat.roomsLoading()).toBe(false);
    chat.loadRooms();
    http.expectOne('/api/rooms').flush({ rooms });
    expect(chat.roomError()).toBe('');
    expect(chat.rooms()).toEqual(rooms);
  });
  it('rejects malformed room payloads with recoverable feedback', () => {
    chat.initializeBrowser();
    http.expectOne('/api/rooms').flush({ rooms: [{ id: 9 }] });
    expect(chat.rooms()).toEqual([]);
    expect(chat.roomError()).toContain('Retry');
  });
  it('encodes guest query fields and derives the socket origin', () => {
    join();
    const url = new URL(urls[0]);
    expect(url.protocol).toBe(location.protocol === 'https:' ? 'wss:' : 'ws:');
    expect(url.host).toBe(location.host);
    expect(url.pathname).toBe('/ws');
    expect(url.searchParams.get('name')).toBe('A & 😀');
    expect(url.searchParams.get('room')).toBe('general');
  });
  it('waits for the atomic ready event before enabling sends', () => {
    const socket = join();
    expect(chat.state()).toBe('connecting');
    socket.open();
    expect(chat.state()).toBe('connecting');
    expect(chat.send('hello')).toBe(false);
    ready(socket, [message('1')]);
    expect(chat.state()).toBe('connected');
    expect(chat.sessionId()).toBe('me');
    expect(chat.messages()).toEqual([message('1')]);
  });
  it('sends trimmed plain text and displays it only after server acceptance', () => {
    const socket = join();
    ready(socket);
    expect(chat.send('  hello\nworld 😀  ')).toBe(true);
    expect(socket.sent).toEqual([
      JSON.stringify({ type: 'message.send', content: 'hello\nworld 😀' }),
    ]);
    expect(chat.messages()).toEqual([]);
    socket.event({ type: 'message.created', message: message('1') });
    expect(chat.messages()).toEqual([message('1')]);
  });
  it('rejects disconnected and transport-closed sends without queuing', () => {
    expect(chat.send('hello')).toBe(false);
    const socket = join();
    ready(socket);
    socket.disconnect();
    expect(chat.send('hello')).toBe(false);
    expect(socket.sent).toEqual([]);
  });
  it('validates message lengths by trimmed Unicode code points', () => {
    const socket = join();
    ready(socket);
    expect(chat.send(' \n ')).toBe(false);
    expect(chat.error()).toContain('Write a message');
    expect(chat.send('😀'.repeat(1001))).toBe(false);
    expect(chat.error()).toContain('1000');
    expect(chat.send('😀'.repeat(1000))).toBe(true);
    expect(socket.sent).toHaveLength(1);
  });
  it('validates guest names by trimmed Unicode code points and rejects unknown rooms', () => {
    boot();
    expect(chat.join('general', '   ')).toBe(false);
    expect(chat.error()).toContain('display name');
    expect(chat.join('general', '😀'.repeat(33))).toBe(false);
    expect(chat.error()).toContain('32');
    expect(chat.join('unknown', 'Amy')).toBe(false);
    expect(sockets).toHaveLength(0);
    expect(chat.join('go', '😀'.repeat(32))).toBe(true);
  });
  it('deduplicates snapshots and broadcasts and keeps the latest 100 messages', () => {
    const socket = join();
    const history = Array.from({ length: 103 }, (_, i) => message(String(i)));
    ready(socket, [history[0], ...history]);
    expect(chat.messages()).toHaveLength(100);
    expect(chat.messages()[0].id).toBe('3');
    socket.event({ type: 'message.created', message: message('102') });
    expect(chat.messages()).toHaveLength(100);
    socket.event({ type: 'message.created', message: message('103') });
    expect(chat.messages()[0].id).toBe('4');
    expect(chat.messages().at(-1)?.id).toBe('103');
  });
  it('replaces missed history on reconnect rather than appending the prior snapshot', () => {
    vi.useFakeTimers();
    const socket = join();
    ready(socket, [message('old')]);
    socket.disconnect();
    vi.advanceTimersByTime(1000);
    ready(sockets[1], [message('new')]);
    expect(chat.messages()).toEqual([message('new')]);
    expect(chat.state()).toBe('connected');
  });
  it('ignores malformed JSON, invalid shapes, early events and events from other rooms', () => {
    const socket = join();
    socket.open();
    socket.event({ type: 'message.created', message: message('early') });
    socket.raw('{');
    socket.event(null);
    socket.event({
      type: 'session.ready',
      sessionId: 'me',
      room: rooms[0],
      messages: 'bad',
      participants: [],
    });
    expect(chat.state()).toBe('connecting');
    ready(socket);
    for (const data of [
      { type: 'message.created', message: { id: 'x' } },
      { type: 'presence.updated', participants: [null] },
      { type: 'message.created', message: { ...message('bad'), createdAt: 'invalid' } },
      { type: 'message.created', message: message('wrong', 'go') },
      { type: 'session.ready', sessionId: 'bad', room: rooms[2], messages: [], participants: [] },
    ])
      socket.event(data);
    expect(chat.messages()).toEqual([]);
    expect(chat.selectedRoom()?.id).toBe('general');
    expect(chat.participants()).toEqual([{ id: 'me', name: 'A & 😀' }]);
  });
  it('updates participants and shows actionable recoverable server errors', () => {
    const socket = join();
    ready(socket);
    socket.event({ type: 'presence.updated', participants: [{ id: 'other', name: 'Mina' }] });
    expect(chat.participants()).toEqual([{ id: 'other', name: 'Mina' }]);
    socket.event({ type: 'error', code: 'invalid_content', message: 'Invalid content' });
    expect(chat.error()).toContain('1000');
    expect(chat.state()).toBe('connected');
    socket.event({ type: 'error', code: 'origin_not_allowed', message: 'Origin not allowed' });
    expect(chat.error()).toContain('origin');
  });
  it('retries unexpected closes at 1, 2, 4, 8, 15 seconds and caps the delay', () => {
    vi.useFakeTimers();
    let socket = join();
    socket.disconnect();
    expect(chat.state()).toBe('reconnecting');
    expect(chat.error()).toContain('1 second');
    for (const delay of [1000, 2000, 4000, 8000, 15000, 15000]) {
      const count = sockets.length;
      vi.advanceTimersByTime(delay - 1);
      expect(sockets).toHaveLength(count);
      vi.advanceTimersByTime(1);
      expect(sockets).toHaveLength(count + 1);
      socket = sockets.at(-1)!;
      socket.disconnect();
    }
  });
  it('resets backoff after a ready snapshot', () => {
    vi.useFakeTimers();
    const socket = join();
    socket.disconnect();
    vi.advanceTimersByTime(1000);
    sockets[1].disconnect();
    vi.advanceTimersByTime(2000);
    ready(sockets[2]);
    sockets[2].disconnect();
    vi.advanceTimersByTime(1000);
    expect(sockets).toHaveLength(4);
  });
  it('cancels retries on leave and clears the room session', () => {
    vi.useFakeTimers();
    const socket = join();
    ready(socket);
    socket.disconnect();
    chat.leave();
    vi.advanceTimersByTime(60000);
    expect(sockets).toHaveLength(1);
    expect(chat.state()).toBe('disconnected');
    expect(chat.selectedRoom()).toBeNull();
    expect(chat.sessionId()).toBeNull();
    expect(chat.messages()).toEqual([]);
    expect(chat.participants()).toEqual([]);
  });
  it('isolates a switched room and ignores callbacks captured from a replaced socket', () => {
    vi.useFakeTimers();
    const old = join();
    ready(old, [message('old')]);
    const oldMessage = old.onmessage;
    const oldClose = old.onclose;
    old.disconnect();
    expect(chat.join('go', 'A & 😀')).toBe(true);
    expect(chat.messages()).toEqual([]);
    expect(chat.participants()).toEqual([]);
    expect(old.close).toHaveBeenCalled();
    ready(sockets[1], [message('go-message', 'go')], rooms[2]);
    oldMessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'message.created', message: message('stale') }),
      }),
    );
    oldClose?.(new CloseEvent('close'));
    vi.advanceTimersByTime(60000);
    expect(sockets).toHaveLength(2);
    expect(chat.messages()).toEqual([message('go-message', 'go')]);
    expect(chat.state()).toBe('connected');
  });
  it('cleans up active sockets and pending timers when its injector is destroyed', () => {
    vi.useFakeTimers();
    const socket = join();
    socket.disconnect();
    TestBed.resetTestingModule();
    vi.advanceTimersByTime(60000);
    expect(sockets).toHaveLength(1);
    expect(socket.close).toHaveBeenCalled();
  });
  it('gives connection feedback on transport error without claiming success', () => {
    const socket = join();
    socket.onerror?.(new Event('error'));
    expect(chat.state()).toBe('connecting');
    expect(chat.error()).toContain('connect');
    socket.disconnect();
    expect(chat.state()).toBe('reconnecting');
  });
});

describe('ChatService on SSR', () => {
  it('never initializes REST or browser transport on the server', () => {
    const factory = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: PLATFORM_ID, useValue: 'server' },
        { provide: CHAT_SOCKET_FACTORY, useValue: factory },
      ],
    });
    const chat = TestBed.inject(ChatService);
    const http = TestBed.inject(HttpTestingController);
    chat.initializeBrowser();
    chat.loadRooms();
    chat.join('general', 'Amy');
    http.expectNone('/api/rooms');
    expect(factory).not.toHaveBeenCalled();
    expect(chat.state()).toBe('disconnected');
    TestBed.resetTestingModule();
  });
});
