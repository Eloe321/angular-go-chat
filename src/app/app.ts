import { Component, afterNextRender, computed, inject, signal } from '@angular/core';
import { ChatService } from './chat/chat.service';
import { Conversation } from './chat/conversation';

@Component({
  selector: 'app-root',
  imports: [Conversation],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {
  protected readonly chat = inject(ChatService);
  protected readonly guestName = signal('');
  protected readonly roomChoice = signal('general');
  protected readonly chosenRoom = computed(
    () =>
      this.chat.selectedRoom() ?? this.chat.rooms().find((room) => room.id === this.roomChoice()),
  );
  protected readonly status = computed(
    () =>
      ({
        disconnected: 'Disconnected',
        connecting: 'Joining room…',
        connected: 'Connected',
        reconnecting: 'Reconnecting…',
      })[this.chat.state()],
  );

  constructor() {
    afterNextRender(() => this.chat.initializeBrowser());
  }

  protected chooseRoom(id: string): void {
    if (this.chat.selectedRoom()?.id === id) return;
    this.roomChoice.set(id);
    if (this.chat.selectedRoom()) this.chat.join(id, this.chat.name());
  }

  protected join(event: Event): void {
    event.preventDefault();
    this.chat.join(this.roomChoice(), this.guestName());
  }

  protected leave(): void {
    this.chat.leave();
  }
}
