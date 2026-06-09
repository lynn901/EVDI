import type { SignalingMessage } from '../types/signaling'

export class SignalingClient {
  private ws: WebSocket | null = null
  private onMessage: ((msg: SignalingMessage) => void) | null = null
  private reconnectAttempts = 0
  private maxReconnectDelay = 16000
  private url = ''

  connect(url: string, onMessage: (msg: SignalingMessage) => void): void {
    this.onMessage = onMessage
    this.url = url
    this.reconnectAttempts = 0
    this.doConnect()
  }

  private doConnect(): void {
    this.ws = new WebSocket(this.url)

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: SignalingMessage = JSON.parse(event.data)
        this.onMessage?.(msg)
      } catch {
        console.error('Failed to parse signaling message')
      }
    }

    this.ws.onclose = () => {
      const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), this.maxReconnectDelay)
      this.reconnectAttempts++
      setTimeout(() => this.doConnect(), delay)
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  send(msg: SignalingMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  disconnect(): void {
    this.onMessage = null
    this.reconnectAttempts = 999
    this.ws?.close()
    this.ws = null
  }
}
