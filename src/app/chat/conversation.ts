import {
  Component,
  ElementRef,
  InjectionToken,
  afterRenderEffect,
  computed,
  inject,
  input,
  signal,
  viewChild,
} from '@angular/core';
import { ChatService } from './chat.service';
import { Room } from './chat.models';

export interface TimestampFormatters {
  time: Intl.DateTimeFormat;
  detail: Intl.DateTimeFormat;
}
export const CHAT_TIMESTAMP_FORMATTERS = new InjectionToken<TimestampFormatters>(
  'Native timestamp formatters',
  {
    providedIn: 'root',
    factory: () => ({
      time: new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }),
      detail: new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }),
    }),
  },
);

@Component({
  selector: 'app-conversation',
  templateUrl: './conversation.html',
  styleUrl: './conversation.css',
  host: { class: 'conversation', role: 'region', 'aria-label': 'Conversation' },
})
export class Conversation {
  readonly room = input<Room>();
  protected readonly chat = inject(ChatService);
  private readonly timestampFormatters = inject(CHAT_TIMESTAMP_FORMATTERS);
  protected readonly draft = signal('');
  protected readonly composing = signal(false);
  protected readonly nearBottom = signal(true);
  protected readonly messageList = viewChild<ElementRef<HTMLElement>>('messageList');
  protected readonly draftLength = computed(() => Array.from(this.draft().trim()).length);
  private lastRoomId: string | undefined;

  constructor() {
    afterRenderEffect(() => {
      const roomId = this.chat.selectedRoom()?.id;
      if (roomId !== this.lastRoomId) this.nearBottom.set(true);
      this.lastRoomId = roomId;
      this.chat.messages();
      const log = this.messageList()?.nativeElement;
      if (log && this.nearBottom()) log.scrollTop = log.scrollHeight;
    });
  }

  protected send(event?: Event): void {
    event?.preventDefault();
    if (this.chat.send(this.draft())) this.draft.set('');
  }

  protected onComposerKey(event: KeyboardEvent): void {
    if (
      event.key !== 'Enter' ||
      event.shiftKey ||
      event.isComposing ||
      this.composing() ||
      event.keyCode === 229
    )
      return;
    event.preventDefault();
    this.send();
  }

  protected onScroll(): void {
    const log = this.messageList()?.nativeElement;
    if (log) this.nearBottom.set(log.scrollHeight - log.scrollTop - log.clientHeight <= 64);
  }

  protected initial(name: string): string {
    return Array.from(name)[0]?.toUpperCase() ?? '';
  }

  protected timeLabel(createdAt: string): string {
    return this.timestampFormatters.time.format(new Date(createdAt));
  }

  protected timestampTitle(createdAt: string): string {
    return this.timestampFormatters.detail.format(new Date(createdAt));
  }
}
