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
  status: 'online' | 'offline' | 'warning' | 'unknown';
  created_at: string;
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
