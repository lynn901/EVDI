import { create } from 'zustand'
import type { ConnectionState } from '../types/signaling'

type SendInputFn = ((msgType: string, payload: unknown) => void) | null

interface ConnectionStore {
  agentAddress: string
  connectionState: ConnectionState
  mediaStream: MediaStream | null
  errorMessage: string | null
  seqCounter: number
  sendInputFn: SendInputFn

  setAgentAddress: (addr: string) => void
  setConnectionState: (state: ConnectionState) => void
  setMediaStream: (stream: MediaStream | null) => void
  setError: (msg: string | null) => void
  nextSeq: () => number
  setSendInputFn: (fn: SendInputFn) => void
  sendInput: (msgType: string, payload: unknown) => void
  reset: () => void
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  agentAddress: `ws://${window.location.hostname}:8080/ws`,
  connectionState: 'disconnected',
  mediaStream: null,
  errorMessage: null,
  seqCounter: 0,
  sendInputFn: null,

  setAgentAddress: (addr) => set({ agentAddress: addr }),
  setConnectionState: (state) => set({ connectionState: state }),
  setMediaStream: (stream) => set({ mediaStream: stream }),
  setError: (msg) => set({ errorMessage: msg, connectionState: msg ? 'error' : get().connectionState }),
  nextSeq: () => {
    const seq = get().seqCounter + 1
    set({ seqCounter: seq })
    return seq
  },
  setSendInputFn: (fn) => set({ sendInputFn: fn }),
  sendInput: (msgType, payload) => {
    get().sendInputFn?.(msgType, payload)
  },
  reset: () => set({
    connectionState: 'disconnected',
    mediaStream: null,
    errorMessage: null,
    seqCounter: 0,
    sendInputFn: null,
  }),
}))
