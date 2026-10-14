import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { App } from './app';
import { CHAT_SOCKET_FACTORY, ChatService } from './chat/chat.service';
import { ChatSocket, Message, Room } from './chat/chat.models';

class FakeSocket implements ChatSocket {
  readyState = 0;
  onopen: ChatSocket['onopen'] = null;
  onmessage: ChatSocket['onmessage'] = null;
  onclose: ChatSocket['onclose'] = null;
  onerror: ChatSocket['onerror'] = null;
  sent: string[] = [];
  send(data: string): void {
    this.sent.push(data);
  }
  close(): void {
    this.readyState = 3;
  }
  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }
  event(data: unknown): void {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }));
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
const message = (id: string, content = 'Hello', roomId = 'general'): Message => ({
  id,
  content,
  roomId,
  senderId: 'other',
  senderName: '<b>Mina</b>',
  createdAt: '2026-10-14T06:00:00Z',
});

describe('Chat screen', () => {
  let fixture: ComponentFixture<App>;
  let page: HTMLElement;
  let http: HttpTestingController;
  let sockets: FakeSocket[];
  beforeEach(async () => {
    sockets = [];
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        {
          provide: CHAT_SOCKET_FACTORY,
          useValue: () => {
            const socket = new FakeSocket();
            sockets.push(socket);
            return socket;
          },
        },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
  });
  afterEach(() => {
    try {
      fixture?.destroy();
      http.verify();
    } finally {
      TestBed.resetTestingModule();
    }
  });
  async function renderRooms(): Promise<void> {
    fixture = TestBed.createComponent(App);
    page = fixture.nativeElement;
    fixture.detectChanges();
    TestBed.tick();
    http.expectOne('/api/rooms').flush({ rooms });
    fixture.detectChanges();
    await fixture.whenStable();
  }
  function enter(selector: string, text: string): void {
    const input = page.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!;
    expect(input, `Expected input ${selector}`).not.toBeNull();
    input.value = text;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
  }
  function submit(selector: string): void {
    const form = page.querySelector(selector)!;
    expect(form, `Expected form ${selector}`).not.toBeNull();
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    fixture.detectChanges();
  }
  function choose(id: string): void {
    page.querySelector<HTMLButtonElement>(`[data-room="${id}"]`)!.click();
    fixture.detectChanges();
  }
  function ready(socket: FakeSocket, room = rooms[0], history: Message[] = [], name = 'Amy'): void {
    socket.open();
    socket.event({
      type: 'session.ready',
      sessionId: 'me',
      room,
      messages: history,
      participants: [{ id: 'me', name }],
    });
    fixture.detectChanges();
  }
  async function joined(): Promise<FakeSocket> {
    await renderRooms();
    enter('#guest-name', '  Amy  ');
    submit('#guest-form');
    expect(sockets).toHaveLength(1);
    ready(sockets[0]);
    return sockets[0];
  }

  it('starts with a labeled guest form and a public-room invitation', async () => {
    await renderRooms();
    expect(page.querySelector('label[for="guest-name"]')?.textContent ?? '').toContain(
      'Display name',
    );
    expect(page.querySelector('h1')?.textContent ?? '').toContain('Public rooms');
    expect(page.textContent).toContain('Choose a room');
  });

  it('announces connection state and keeps the composer unavailable before joining', async () => {
    await renderRooms();
    expect(page.querySelector('[role="status"]')?.textContent ?? '').toContain('Disconnected');
    expect(page.querySelector<HTMLTextAreaElement>('#message')?.disabled).toBe(true);
  });

  it('loads rooms only after rendering and presents a retry for unavailable rooms', async () => {
    fixture = TestBed.createComponent(App);
    page = fixture.nativeElement;
    http.expectNone('/api/rooms');
    fixture.detectChanges();
    TestBed.tick();
    expect(page.textContent).toContain('Loading rooms');
    http.expectOne('/api/rooms').flush(null, { status: 503, statusText: 'Unavailable' });
    fixture.detectChanges();
    expect(page.textContent).toContain('unavailable');
    page.querySelector<HTMLButtonElement>('[data-action="retry"]')!.click();
    http.expectOne('/api/rooms').flush({ rooms });
    fixture.detectChanges();
    expect(page.querySelectorAll('[data-room]')).toHaveLength(3);
  });
  it('joins the chosen room, waits for ready, switches rooms, and leaves', async () => {
    await renderRooms();
    enter('#guest-name', '  Amy  ');
    choose('angular');
    submit('#guest-form');
    sockets[0].open();
    fixture.detectChanges();
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.disabled).toBe(true);
    ready(sockets[0], rooms[1]);
    expect(page.querySelector('[role="status"]')?.textContent).toContain('Connected');
    expect(page.textContent).toContain('Amy');
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.disabled).toBe(false);
    choose('go');
    expect(sockets[0].readyState).toBe(3);
    ready(sockets[1], rooms[2]);
    expect(page.querySelector('.conversation h2')?.textContent).toContain('Go');
    page.querySelector<HTMLButtonElement>('[data-action="leave"]')!.click();
    fixture.detectChanges();
    expect(sockets[1].readyState).toBe(3);
    expect(page.querySelector('#guest-form')).not.toBeNull();
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.disabled).toBe(true);
  });
  it('shows name validation and accepts 32 Unicode code points without HTML maxlength', async () => {
    await renderRooms();
    enter('#guest-name', ' ');
    submit('#guest-form');
    expect(page.textContent).toContain('display name');
    expect(sockets).toHaveLength(0);
    enter('#guest-name', '😀'.repeat(33));
    submit('#guest-form');
    expect(sockets).toHaveLength(0);
    expect(page.querySelector('#guest-name')!.hasAttribute('maxlength')).toBe(false);
    enter('#guest-name', '😀'.repeat(32));
    submit('#guest-form');
    expect(sockets).toHaveLength(1);
  });
  it('renders participant names and message content as escaped text', async () => {
    const socket = await joined();
    const content = '<img src=x onerror="alert(1)">';
    socket.event({ type: 'message.created', message: message('safe', content) });
    socket.event({
      type: 'presence.updated',
      participants: [{ id: 'other', name: '<b>Mina</b>' }],
    });
    fixture.detectChanges();
    expect(page.textContent).toContain(content);
    expect(page.textContent).toContain('<b>Mina</b>');
    expect(page.querySelector('img')).toBeNull();
    expect(page.querySelector('.message b')).toBeNull();
    expect(page.querySelector('time')?.getAttribute('datetime')).toBe('2026-10-14T06:00:00Z');
  });
  it('keeps a draft through disconnection and never sends while disconnected', async () => {
    const socket = await joined();
    enter('#message', 'Keep this draft');
    socket.disconnect();
    fixture.detectChanges();
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.value).toBe('Keep this draft');
    expect(page.querySelector<HTMLButtonElement>('[data-action="send"]')!.disabled).toBe(true);
    submit('#composer');
    expect(socket.sent).toEqual([]);
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.value).toBe('Keep this draft');
    expect(page.querySelector('[role="status"]')?.textContent).toContain('Reconnecting');
  });
  it('uses a complete Unicode character for guest initials', async () => {
    const socket = await joined();
    socket.event({
      type: 'message.created',
      message: { ...message('emoji'), senderName: '😀 Amy' },
    });
    fixture.detectChanges();
    expect(page.querySelector('.avatar')?.textContent).toBe('😀');
  });
  it('shows blank and overlong message errors, accepts Unicode, and clears only sent drafts', async () => {
    const socket = await joined();
    enter('#message', ' \n ');
    submit('#composer');
    expect(page.textContent).toContain('Write a message');
    expect(socket.sent).toEqual([]);
    enter('#message', '😀'.repeat(1001));
    submit('#composer');
    expect(page.textContent).toContain('1000');
    expect(page.querySelector('#message')!.hasAttribute('maxlength')).toBe(false);
    enter('#message', '😀'.repeat(1000));
    submit('#composer');
    expect(socket.sent).toHaveLength(1);
    expect(page.querySelector<HTMLTextAreaElement>('#message')!.value).toBe('');
    expect(page.querySelectorAll('.message')).toHaveLength(0);
  });
  it('sends on Enter and preserves Shift+Enter and input-method composition', async () => {
    const socket = await joined();
    enter('#message', 'First');
    const input = page.querySelector<HTMLTextAreaElement>('#message')!;
    const key = (options: KeyboardEventInit) => {
      const event = new KeyboardEvent('keydown', {
        key: 'Enter',
        bubbles: true,
        cancelable: true,
        ...options,
      });
      input.dispatchEvent(event);
      fixture.detectChanges();
      return event;
    };
    expect(key({ shiftKey: true }).defaultPrevented).toBe(false);
    expect(socket.sent).toHaveLength(0);
    expect(key({ isComposing: true }).defaultPrevented).toBe(false);
    expect(socket.sent).toHaveLength(0);
    input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
    key({});
    expect(socket.sent).toHaveLength(0);
    input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }));
    expect(key({}).defaultPrevented).toBe(true);
    expect(socket.sent).toHaveLength(1);
    expect(JSON.parse(socket.sent[0])).toEqual({ type: 'message.send', content: 'First' });
  });
  it('scrolls new messages only while the reader is near the bottom', async () => {
    const socket = await joined();
    const log = page.querySelector<HTMLElement>('[role="log"]')!;
    Object.defineProperty(log, 'clientHeight', { value: 100, configurable: true });
    Object.defineProperty(log, 'scrollHeight', { value: 500, configurable: true });
    log.scrollTop = 0;
    log.dispatchEvent(new Event('scroll'));
    socket.event({ type: 'message.created', message: message('1') });
    fixture.detectChanges();
    await fixture.whenStable();
    expect(log.scrollTop).toBe(0);
    log.scrollTop = 400;
    log.dispatchEvent(new Event('scroll'));
    Object.defineProperty(log, 'scrollHeight', { value: 600, configurable: true });
    socket.event({ type: 'message.created', message: message('2') });
    fixture.detectChanges();
    await fixture.whenStable();
    expect(log.scrollTop).toBe(600);
  });
});
