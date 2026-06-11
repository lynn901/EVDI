import { create } from 'zustand'
import type { ConnectionState } from '../types/signaling'

interface ConnectionStore {
  agentAddress: string
  connectionState: ConnectionState
  mediaStream: MediaStream | null
  errorMessage: string | null
  seqCounter: number

  setAgentAddress: (addr: string) => void
  setConnectionState: (state: ConnectionState) => void
  setMediaStream: (stream: MediaStream | null) => void
  setError: (msg: string | null) => void
  nextSeq: () => number
  reset: () => void
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  agentAddress: 'ws://172.26.185.252:8080/ws',
  connectionState: 'disconnected',
  mediaStream: null,
  errorMessage: null,
  seqCounter: 0,

  setAgentAddress: (addr) => set({ agentAddress: addr }),
  setConnectionState: (state) => set({ connectionState: state }),
  setMediaStream: (stream) => set({ mediaStream: stream }),
  setError: (msg) => set({ errorMessage: msg, connectionState: msg ? 'error' : get().connectionState }),
  nextSeq: () => {
    const seq = get().seqCounter + 1
    set({ seqCounter: seq })
    return seq
  },
  reset: () => set({
    connectionState: 'disconnected',
    mediaStream: null,
    errorMessage: null,
    seqCounter: 0,
  }),
}))
