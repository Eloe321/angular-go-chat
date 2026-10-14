export interface Room {
  id: string;
  name: string;
  description: string;
}
export interface Participant {
  id: string;
  name: string;
}
export interface Message {
  id: string;
  roomId: string;
  senderId: string;
  senderName: string;
  content: string;
  createdAt: string;
}
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';
export type ServerEvent =
  | {
      type: 'session.ready';
      sessionId: string;
      room: Room;
      messages: Message[];
      participants: Participant[];
    }
  | { type: 'message.created'; message: Message }
  | { type: 'presence.updated'; participants: Participant[] }
  | { type: 'error'; code: string; message: string };
export interface SendMessage {
  type: 'message.send';
  content: string;
}
export interface ChatSocket {
  readonly readyState: number;
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  send(data: string): void;
  close(): void;
}
export type SocketFactory = (url: string) => ChatSocket;
