import { DOCUMENT, isPlatformBrowser } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { DestroyRef, Injectable, InjectionToken, PLATFORM_ID, inject, signal } from '@angular/core';
import {
  ChatSocket,
  ConnectionState,
  Message,
  Participant,
  Room,
  ServerEvent,
  SocketFactory,
} from './chat.models';

export const CHAT_SOCKET_FACTORY = new InjectionToken<SocketFactory>(
  'Chat browser socket factory',
  {
    providedIn: 'root',
    factory:
      () =>
      (url: string): ChatSocket =>
        new WebSocket(url),
  },
);

@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly http = inject(HttpClient);
  private readonly destroyRef = inject(DestroyRef);
  private readonly platformId = inject(PLATFORM_ID);
  private readonly document = inject(DOCUMENT);
  private readonly socketFactory = inject(CHAT_SOCKET_FACTORY);
  private socket: ChatSocket | null = null;
  private browserReady = false;
  private generation = 0;
  private retryAttempt = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private destroyed = false;
  readonly rooms = signal<Room[]>([]);
  readonly roomsLoading = signal(false);
  readonly roomError = signal('');
  readonly selectedRoom = signal<Room | null>(null);
  readonly messages = signal<Message[]>([]);
  readonly participants = signal<Participant[]>([]);
  readonly sessionId = signal<string | null>(null);
  readonly name = signal('');
  readonly state = signal<ConnectionState>('disconnected');
  readonly error = signal('');

  constructor() {
    this.destroyRef.onDestroy(() => {
      this.destroyed = true;
      this.stopConnection();
    });
  }

  initializeBrowser(): void {
    if (!isPlatformBrowser(this.platformId) || this.browserReady || this.destroyed) return;
    this.browserReady = true;
    this.loadRooms();
  }
  loadRooms(): void {
    if (!this.browserReady || this.roomsLoading() || this.destroyed) return;
    this.roomsLoading.set(true);
    this.roomError.set('');
    this.http
      .get<unknown>('/api/rooms')
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (data) => {
          this.roomsLoading.set(false);
          if (
            record(data) &&
            Array.isArray(data['rooms']) &&
            data['rooms'].length > 0 &&
            data['rooms'].every(isRoom)
          ) {
            this.rooms.set(data['rooms']);
          } else this.roomError.set('Rooms could not be loaded. Retry to try again.');
        },
        error: () => {
          this.roomsLoading.set(false);
          this.roomError.set('The chat service is unavailable. Retry to load rooms.');
        },
      });
  }
  join(roomId: string, name: string): boolean {
    if (!this.browserReady || this.destroyed) return false;
    const trimmed = name.trim();
    if (Array.from(trimmed).length < 1 || Array.from(trimmed).length > 32) {
      this.error.set('Enter a display name with 1 to 32 characters.');
      return false;
    }
    const room = this.rooms().find((candidate) => candidate.id === roomId);
    if (!room) {
      this.error.set('Choose an available room.');
      return false;
    }
    this.stopConnection();
    this.retryAttempt = 0;
    this.error.set('');
    this.name.set(trimmed);
    this.selectedRoom.set(room);
    this.messages.set([]);
    this.participants.set([]);
    this.sessionId.set(null);
    this.state.set('connecting');
    this.connect();
    return true;
  }

  leave(): void {
    this.stopConnection();
    this.state.set('disconnected');
    this.selectedRoom.set(null);
    this.messages.set([]);
    this.participants.set([]);
    this.sessionId.set(null);
    this.name.set('');
    this.error.set('');
  }

  send(content: string): boolean {
    if (this.state() !== 'connected' || this.socket?.readyState !== 1) {
      this.error.set('Wait for a connection before sending. Your draft is kept.');
      return false;
    }
    const trimmed = content.trim();
    const length = Array.from(trimmed).length;
    if (length < 1) {
      this.error.set('Write a message before sending.');
      return false;
    }
    if (length > 1000) {
      this.error.set('Keep your message within 1000 characters.');
      return false;
    }
    try {
      this.socket.send(JSON.stringify({ type: 'message.send', content: trimmed }));
      this.error.set('');
      return true;
    } catch {
      this.error.set(
        'The message could not be sent. Your draft is kept; check the connection and try again.',
      );
      return false;
    }
  }

  private connect(): void {
    const room = this.selectedRoom();
    if (!room || this.destroyed) return;
    this.releaseSocket();
    const generation = ++this.generation;
    const url = new URL('/ws', this.document.location.origin);
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    url.searchParams.set('room', room.id);
    url.searchParams.set('name', this.name());
    try {
      const socket = this.socketFactory(url.toString());
      this.socket = socket;
      const current = () =>
        !this.destroyed && this.generation === generation && this.socket === socket;
      // A transport open is not a registered chat session. Only session.ready enables sending.
      socket.onopen = () => {};
      socket.onmessage = (event) => {
        if (!current()) return;
        const data = parseEvent(event.data);
        if (!data) return;
        if (data.type === 'error') {
          this.error.set(serverError(data.code, data.message));
          return;
        }
        if (data.type === 'session.ready') {
          if (
            this.state() === 'connected' ||
            data.room.id !== room.id ||
            data.messages.some((message) => message.roomId !== room.id)
          )
            return;
          this.selectedRoom.set(data.room);
          this.messages.set(latestMessages(data.messages));
          this.participants.set(data.participants);
          this.sessionId.set(data.sessionId);
          this.state.set('connected');
          this.retryAttempt = 0;
          this.error.set('');
        } else if (this.state() === 'connected') {
          if (data.type === 'message.created' && data.message.roomId === room.id) {
            this.messages.update((messages) => latestMessages([...messages, data.message]));
          } else if (data.type === 'presence.updated') this.participants.set(data.participants);
        }
      };
      socket.onerror = () => {
        if (current())
          this.error.set(
            'Unable to connect to chat. Check that the service is running and this site is an allowed origin.',
          );
      };
      socket.onclose = () => {
        if (current()) this.scheduleRetry(generation);
      };
    } catch {
      this.scheduleRetry(generation);
    }
  }

  private scheduleRetry(generation: number): void {
    if (this.retryTimer !== null || this.destroyed || !this.selectedRoom()) return;
    this.state.set('reconnecting');
    this.sessionId.set(null);
    this.participants.set([]);
    const delay = [1, 2, 4, 8, 15][Math.min(this.retryAttempt++, 4)];
    this.error.set(
      `Connection interrupted. Retrying in ${delay} second${delay === 1 ? '' : 's'}. Your draft is kept. Check the chat service if retries continue.`,
    );
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      if (this.generation === generation && !this.destroyed) this.connect();
    }, delay * 1000);
  }

  private stopConnection(): void {
    this.generation++;
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.releaseSocket();
  }

  private releaseSocket(): void {
    if (!this.socket) return;
    const socket = this.socket;
    this.socket = null;
    socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
    socket.close();
  }
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function isRoom(value: unknown): value is Room {
  return (
    record(value) &&
    typeof value['id'] === 'string' &&
    ['general', 'angular', 'go'].includes(value['id']) &&
    typeof value['name'] === 'string' &&
    value['name'].length > 0 &&
    typeof value['description'] === 'string'
  );
}

function text(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}
function isParticipant(value: unknown): value is Participant {
  return (
    record(value) &&
    text(value['id']) &&
    text(value['name']) &&
    Array.from(value['name'].trim()).length > 0 &&
    Array.from(value['name']).length <= 32
  );
}
function isMessage(value: unknown): value is Message {
  return (
    record(value) &&
    text(value['id']) &&
    text(value['roomId']) &&
    text(value['senderId']) &&
    text(value['senderName']) &&
    text(value['content']) &&
    Array.from(value['content']).length <= 1000 &&
    text(value['createdAt']) &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/.test(value['createdAt']) &&
    Number.isFinite(Date.parse(value['createdAt']))
  );
}
function parseEvent(raw: unknown): ServerEvent | null {
  if (typeof raw !== 'string') return null;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!record(data)) return null;
  switch (data['type']) {
    case 'session.ready':
      if (
        text(data['sessionId']) &&
        isRoom(data['room']) &&
        Array.isArray(data['messages']) &&
        data['messages'].every(isMessage) &&
        Array.isArray(data['participants']) &&
        data['participants'].every(isParticipant)
      ) {
        return {
          type: 'session.ready',
          sessionId: data['sessionId'],
          room: data['room'],
          messages: data['messages'],
          participants: data['participants'],
        };
      }
      return null;
    case 'message.created':
      return isMessage(data['message'])
        ? { type: 'message.created', message: data['message'] }
        : null;
    case 'presence.updated':
      return Array.isArray(data['participants']) && data['participants'].every(isParticipant)
        ? { type: 'presence.updated', participants: data['participants'] }
        : null;
    case 'error':
      return text(data['code']) && text(data['message'])
        ? { type: 'error', code: data['code'], message: data['message'] }
        : null;
    default:
      return null;
  }
}
function latestMessages(messages: Message[]): Message[] {
  const ids = new Set<string>();
  return messages
    .filter((message) => {
      if (ids.has(message.id)) return false;
      ids.add(message.id);
      return true;
    })
    .slice(-100);
}
function serverError(code: string, fallback: string): string {
  switch (code) {
    case 'invalid_content':
      return 'Write a message with 1 to 1000 characters, then try again.';
    case 'invalid_name':
      return 'Use a display name with 1 to 32 characters and join again.';
    case 'origin_not_allowed':
      return 'This site origin is not allowed to connect. Use an approved chat address.';
    default:
      return fallback;
  }
}
