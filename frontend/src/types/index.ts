export interface User {
  user_id: number;
  username: string;
  is_admin: boolean;
}

export interface Server {
  id: number;
  name: string;
  type: 'linux' | 'windows';
  host: string;
  port: number;
  connection_type: 'ssh' | 'ftp';
  username?: string;
  password?: string;       // only used on create/update, never returned
  ssh_key_path?: string;   // only used on create/update, never returned
  use_tls?: boolean;
  status: 'online' | 'offline' | 'warning' | 'unknown';
  created_at: string;
}

export interface BackupSource {
  id: number;
  server_id: number;
  name: string;
  type: 'web' | 'database' | 'config';
  source_path?: string;
  db_name?: string;
  depends_on?: number;
  priority: number;
  enabled: boolean;
  exclude_patterns?: string;
  created_at: string;
}

export interface DiscoveredService {
  name: string;
  data: Record<string, unknown>;
}

export interface DiscoveryResult {
  server_id: number;
  services: DiscoveredService[];
  scanned_at: string;
}

export interface BackupRun {
  id: number;
  job_id: number;
  status: 'pending' | 'running' | 'success' | 'failed' | 'timeout';
  started_at: string;
  finished_at: string;
  total_size_bytes: number;
  files_copied: number;
  error_message?: string;
}
