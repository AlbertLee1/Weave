import { request } from './client';

// ServerInfo mirrors pkg/serverinfo handler.go Response — the running
// process's uptime and runtime stats.
export interface ServerInfo {
  startedAt: string;
  uptimeSeconds: number;
  goroutineCount: number;
  memoryAllocBytes: number;
  memorySysBytes: number;
  gcCycles: number;
}

export interface PostgresStats {
  acquiredConns: number;
  idleConns: number;
  totalConns: number;
  maxConns: number;
  newConnsCount: number;
}

export interface NATSStats {
  status: string;
  serverUrl?: string;
  inMsgs: number;
  outMsgs: number;
  reconnects: number;
}

// ConnectionStats mirrors pkg/serverinfo connections.go — pointers are
// null when the backing service isn't wired.
export interface ConnectionStats {
  postgres: PostgresStats | null;
  nats: NATSStats | null;
}

export function getServerInfo(): Promise<ServerInfo> {
  return request<ServerInfo>('GET', '/api/v2/server-info');
}

export function getServerConnections(): Promise<ConnectionStats> {
  return request<ConnectionStats>('GET', '/api/v2/server-info/connections');
}
