import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  X,
  Monitor,
  Server,
  ChevronRight,
  ChevronLeft,
  Loader2,
  CheckCircle2,
  AlertCircle,
  Copy,
  Check,
  Plus,
  Trash2,
} from 'lucide-react';
import { serversApi } from '../api/servers';
import type { DiscoveryResult, DiscoveredService } from '../types';

interface WizardProps {
  onClose: () => void;
}

type ServerTypeChoice = 'linux' | 'windows' | null;
type AuthMethod = 'password' | 'key';

interface ConnectionForm {
  name: string;
  host: string;
  port: string;
  username: string;
  password: string;
  authMethod: AuthMethod;
  privateKey: string;
}

interface SourceEntry {
  name: string;
  type: 'web' | 'database' | 'config';
  source_path: string;
}

const defaultConn: ConnectionForm = {
  name: '',
  host: '',
  port: '22',
  username: '',
  password: '',
  authMethod: 'password',
  privateKey: '',
};

const defaultWinConn: ConnectionForm = {
  name: '',
  host: '',
  port: '21',
  username: '',
  password: '',
  authMethod: 'password',
  privateKey: '',
};

// ── Stepper ──────────────────────────────────────────────────────────────────

function Stepper({ steps, current }: { steps: string[]; current: number }) {
  return (
    <div className="flex items-center gap-0 mb-8">
      {steps.map((label, i) => (
        <div key={i} className="flex items-center flex-1 last:flex-none">
          <div className="flex flex-col items-center">
            <div
              className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold border-2 transition-colors ${
                i < current
                  ? 'bg-blue-600 border-blue-600 text-white'
                  : i === current
                  ? 'border-blue-600 text-blue-600 bg-white'
                  : 'border-gray-300 text-gray-400 bg-white'
              }`}
            >
              {i < current ? <Check size={13} /> : i + 1}
            </div>
            <span
              className={`text-xs mt-1 text-center leading-tight ${
                i === current ? 'text-blue-600 font-semibold' : 'text-gray-400'
              }`}
            >
              {label}
            </span>
          </div>
          {i < steps.length - 1 && (
            <div
              className={`flex-1 h-0.5 mx-1 mb-4 ${
                i < current ? 'bg-blue-600' : 'bg-gray-200'
              }`}
            />
          )}
        </div>
      ))}
    </div>
  );
}

// ── CopyBox ───────────────────────────────────────────────────────────────────

function CopyBox({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="relative bg-gray-900 text-green-400 text-xs font-mono rounded-lg p-3 pr-10 overflow-x-auto">
      <pre className="whitespace-pre-wrap break-all">{text}</pre>
      <button
        onClick={copy}
        className="absolute top-2 right-2 text-gray-400 hover:text-white transition-colors"
        title="Copy"
      >
        {copied ? <Check size={14} /> : <Copy size={14} />}
      </button>
    </div>
  );
}

// ── Main Wizard ───────────────────────────────────────────────────────────────

export default function AddServerWizard({ onClose }: WizardProps) {
  const queryClient = useQueryClient();

  // shared
  const [serverType, setServerType] = useState<ServerTypeChoice>(null);
  const [step, setStep] = useState(0);

  // linux state
  const [conn, setConn] = useState<ConnectionForm>(defaultConn);
  const [testStatus, setTestStatus] = useState<'idle' | 'loading' | 'ok' | 'error'>('idle');
  const [testMsg, setTestMsg] = useState('');
  const [discovery, setDiscovery] = useState<DiscoveryResult | null>(null);
  const [discoveryLoading, setDiscoveryLoading] = useState(false);
  const [selectedServices, setSelectedServices] = useState<Record<string, boolean>>({});
  const [selectedSources, setSelectedSources] = useState<Record<string, boolean>>({});
  const [mysqlUser, setMysqlUser] = useState('backup_user');
  const [mysqlPass, setMysqlPass] = useState('SecurePass123!');

  // windows state
  const [winConn, setWinConn] = useState<ConnectionForm>(defaultWinConn);
  const [winTestStatus, setWinTestStatus] = useState<'idle' | 'loading' | 'ok' | 'error'>('idle');
  const [winTestMsg, setWinTestMsg] = useState('');
  const [manualSources, setManualSources] = useState<SourceEntry[]>([
    { name: '', type: 'web', source_path: '' },
  ]);

  const createServer = useMutation({
    mutationFn: async () => {
      if (serverType === 'linux') {
        const server = await serversApi.create({
          name: conn.name,
          host: conn.host,
          port: parseInt(conn.port),
          type: 'linux',
          connection_type: 'ssh',
          username: conn.username,
          password: conn.password,
          ssh_key_path: conn.authMethod === 'key' ? conn.privateKey : undefined,
          status: 'unknown',
        } as never);
        // create sources from selected
        const sourceEntries = buildLinuxSources();
        for (const src of sourceEntries) {
          await serversApi.createSource(server.id, src);
        }
      } else {
        const server = await serversApi.create({
          name: winConn.name,
          host: winConn.host,
          port: parseInt(winConn.port),
          type: 'windows',
          connection_type: 'ftp',
          username: winConn.username,
          password: winConn.password,
          status: 'unknown',
        } as never);
        for (const src of manualSources.filter((s) => s.name && s.source_path)) {
          await serversApi.createSource(server.id, src);
        }
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['servers'] });
      onClose();
    },
  });

  // ── Helpers ────────────────────────────────────────────────────────────────

  const getService = (name: string): DiscoveredService | undefined =>
    discovery?.services.find((s) => s.name === name);

  const buildLinuxSources = () => {
    const sources: Array<{ name: string; type: string; source_path?: string; db_name?: string }> = [];
    if (!discovery) return sources;

    // web sources
    const nginx = getService('nginx');
    if (nginx && selectedSources['nginx_config']) {
      sources.push({ name: 'NGINX Config', type: 'config', source_path: '/etc/nginx' });
    }
    const vhosts = nginx?.data?.vhosts as Array<{ name: string; root_path: string }> | undefined;
    if (vhosts) {
      vhosts.forEach((vh, i) => {
        const key = `vhost_${i}`;
        if (selectedSources[key] && vh.root_path) {
          sources.push({ name: vh.name || `vhost_${i}`, type: 'web', source_path: vh.root_path });
        }
      });
    }

    // databases
    const mysql = getService('mysql');
    const databases = mysql?.data?.databases as string[] | undefined;
    if (databases) {
      databases.forEach((db) => {
        if (selectedSources[`db_${db}`]) {
          sources.push({ name: db, type: 'database', db_name: db });
        }
      });
    }

    // certbot
    const certbot = getService('certbot');
    if (certbot && selectedSources['certbot']) {
      sources.push({ name: 'Certbot Certs', type: 'config', source_path: '/etc/letsencrypt' });
    }

    // pm2
    const pm2 = getService('pm2');
    if (pm2 && selectedSources['pm2']) {
      sources.push({ name: 'PM2 Config', type: 'config', source_path: '/etc/pm2' });
    }

    // redis RDB dump
    const redis = getService('redis');
    if (redis && selectedSources['redis']) {
      const rdbPath = (redis.data?.rdb_path as string) || '/var/lib/redis/dump.rdb';
      sources.push({ name: 'Redis Data', type: 'config', source_path: rdbPath });
    }

    return sources;
  };

  const hasDatabases = () => {
    const mysql = getService('mysql');
    const databases = mysql?.data?.databases as string[] | undefined;
    return databases && databases.length > 0 && databases.some((db) => selectedSources[`db_${db}`]);
  };

  // ── Linux steps ────────────────────────────────────────────────────────────

  const LINUX_STEPS = ['Type', 'Connection', 'Discovery', 'Sources', 'MySQL', 'Summary'];
  const WIN_STEPS = ['Type', 'Connection', 'Sources', 'Summary'];

  const handleTestConnection = async (isWindows = false) => {
    const c = isWindows ? winConn : conn;
    const setStatus = isWindows ? setWinTestStatus : setTestStatus;
    const setMsg = isWindows ? setWinTestMsg : setTestMsg;
    setStatus('loading');
    try {
      const res = await serversApi.testConnection({
        host: c.host,
        port: parseInt(c.port),
        connection_type: isWindows ? 'ftp' : 'ssh',
        username: c.username,
        password: c.password,
        private_key: c.authMethod === 'key' ? c.privateKey : undefined,
      });
      setStatus(res.success ? 'ok' : 'error');
      setMsg(res.message);
    } catch (e: unknown) {
      setStatus('error');
      setMsg(e instanceof Error ? e.message : 'Connection failed');
    }
  };

  const handleRunDiscovery = async () => {
    setDiscoveryLoading(true);
    try {
      const res = await serversApi.discoverPreview({
        host: conn.host,
        port: parseInt(conn.port) || 22,
        username: conn.username,
        password: conn.password,
        ssh_key_path: conn.authMethod === 'key' ? conn.privateKey : undefined,
      });
      setDiscovery(res);
      // pre-select all
      const sel: Record<string, boolean> = {};
      const dr = res as DiscoveryResult;
      if (dr?.services) {
        dr.services.forEach((svc) => {
          sel[svc.name] = true;
        });
      }
      setSelectedServices(sel);
    } catch {
      setDiscovery({ server_id: 0, services: [], scanned_at: new Date().toISOString() });
    } finally {
      setDiscoveryLoading(false);
    }
  };

  const toggleSource = (key: string) =>
    setSelectedSources((prev) => ({ ...prev, [key]: !prev[key] }));

  // ── Render steps ───────────────────────────────────────────────────────────

  const renderStep0 = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">Choose Server Type</h2>
      <p className="text-sm text-gray-500 mb-6">Select the operating system of the server you want to add.</p>
      <div className="grid grid-cols-2 gap-4">
        <button
          onClick={() => { setServerType('linux'); setStep(1); setConn({ ...defaultConn }); }}
          className="border-2 rounded-xl p-6 text-left hover:border-blue-500 hover:bg-blue-50 transition-all group"
        >
          <Monitor size={32} className="text-green-600 mb-3" />
          <div className="font-bold text-gray-900 text-base">Linux</div>
          <div className="text-sm text-gray-500 mt-1">SSH connection, auto-discovery of services</div>
        </button>
        <button
          onClick={() => { setServerType('windows'); setStep(1); setWinConn({ ...defaultWinConn }); }}
          className="border-2 rounded-xl p-6 text-left hover:border-blue-500 hover:bg-blue-50 transition-all group"
        >
          <Server size={32} className="text-blue-600 mb-3" />
          <div className="font-bold text-gray-900 text-base">Windows</div>
          <div className="text-sm text-gray-500 mt-1">FTP connection, manual source configuration</div>
        </button>
      </div>
    </div>
  );

  // Linux Step 1: Connection
  const renderLinuxConnection = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">SSH Connection</h2>
      <p className="text-sm text-gray-500 mb-5">Enter the connection details for your Linux server.</p>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Server Name *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="My Production Server"
              value={conn.name}
              onChange={(e) => setConn({ ...conn, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Host / IP *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="192.168.1.10"
              value={conn.host}
              onChange={(e) => setConn({ ...conn, host: e.target.value })}
            />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">SSH Port</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="22"
              value={conn.port}
              onChange={(e) => setConn({ ...conn, port: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Username *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="root"
              value={conn.username}
              onChange={(e) => setConn({ ...conn, username: e.target.value })}
            />
          </div>
        </div>
        <div>
          <label className="block text-xs font-semibold text-gray-600 mb-1">Authentication Method</label>
          <div className="flex gap-4">
            {(['password', 'key'] as AuthMethod[]).map((m) => (
              <label key={m} className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="authMethod"
                  value={m}
                  checked={conn.authMethod === m}
                  onChange={() => setConn({ ...conn, authMethod: m })}
                  className="text-blue-600"
                />
                <span className="text-sm capitalize">{m === 'key' ? 'Private Key' : 'Password'}</span>
              </label>
            ))}
          </div>
        </div>
        {conn.authMethod === 'password' ? (
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Password *</label>
            <input
              type="password"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              value={conn.password}
              onChange={(e) => setConn({ ...conn, password: e.target.value })}
            />
          </div>
        ) : (
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Private Key (PEM)</label>
            <textarea
              rows={4}
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;..."
              value={conn.privateKey}
              onChange={(e) => setConn({ ...conn, privateKey: e.target.value })}
            />
          </div>
        )}
        <div className="flex items-center gap-3">
          <button
            onClick={() => handleTestConnection(false)}
            disabled={testStatus === 'loading' || !conn.host || !conn.username}
            className="inline-flex items-center gap-2 px-4 py-2 bg-gray-100 hover:bg-gray-200 text-gray-700 text-sm rounded-lg font-medium transition-colors disabled:opacity-50"
          >
            {testStatus === 'loading' && <Loader2 size={14} className="animate-spin" />}
            Test Connection
          </button>
          {testStatus === 'ok' && (
            <span className="flex items-center gap-1.5 text-green-600 text-sm">
              <CheckCircle2 size={15} /> {testMsg || 'Connected successfully'}
            </span>
          )}
          {testStatus === 'error' && (
            <span className="flex items-center gap-1.5 text-red-600 text-sm">
              <AlertCircle size={15} /> {testMsg || 'Connection failed'}
            </span>
          )}
        </div>
      </div>
    </div>
  );

  // Linux Step 2: Auto-Discovery
  const renderDiscovery = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">Auto-Discovery</h2>
      <p className="text-sm text-gray-500 mb-5">
        Scan your server to automatically detect installed services and applications.
      </p>
      {!discovery && !discoveryLoading && (
        <button
          onClick={handleRunDiscovery}
          className="px-5 py-2.5 bg-blue-600 text-white text-sm rounded-lg font-medium hover:bg-blue-700 transition-colors"
        >
          Start Scan
        </button>
      )}
      {discoveryLoading && (
        <div className="flex items-center gap-3 py-8 justify-center">
          <Loader2 size={22} className="animate-spin text-blue-600" />
          <span className="text-gray-600">Scanning server...</span>
        </div>
      )}
      {discovery && !discoveryLoading && (
        <div>
          {!discovery.services || discovery.services.length === 0 ? (
            <div className="text-gray-500 text-sm py-4">No services detected. You can proceed to configure sources manually.</div>
          ) : (
            <div className="space-y-3">
              <p className="text-sm font-medium text-gray-700">Found {discovery.services.length} service(s):</p>
              {discovery.services.map((svc) => (
                <div key={svc.name} className="border border-gray-200 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <input
                      type="checkbox"
                      id={`svc_${svc.name}`}
                      checked={!!selectedServices[svc.name]}
                      onChange={() =>
                        setSelectedServices((prev) => ({ ...prev, [svc.name]: !prev[svc.name] }))
                      }
                      className="text-blue-600"
                    />
                    <label htmlFor={`svc_${svc.name}`} className="font-semibold text-sm capitalize">
                      {svc.name}
                    </label>
                  </div>
                  <div className="pl-6 text-xs text-gray-500">
                    {Object.entries(svc.data).map(([k, v]) => (
                      <div key={k}>
                        <span className="font-medium">{k}:</span> {JSON.stringify(v)}
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
          <button
            onClick={handleRunDiscovery}
            className="mt-4 text-sm text-blue-600 hover:underline"
          >
            Re-scan
          </button>
        </div>
      )}
    </div>
  );

  // Linux Step 3: Source Selection
  const renderSourceSelection = () => {
    const nginx = getService('nginx');
    const vhosts = nginx?.data?.vhosts as Array<{ name: string; root_path: string }> | undefined;
    const mysql = getService('mysql');
    const databases = mysql?.data?.databases as string[] | undefined;
    const redis = getService('redis');
    const certbot = getService('certbot');
    const pm2 = getService('pm2');

    const hasAnything = nginx || mysql || redis || certbot || pm2;

    return (
      <div>
        <h2 className="text-lg font-bold text-gray-900 mb-1">Source Selection</h2>
        <p className="text-sm text-gray-500 mb-5">Choose what to include in your backups.</p>
        {!hasAnything ? (
          <p className="text-gray-500 text-sm">No services were discovered. You can add sources manually after saving.</p>
        ) : (
          <div className="space-y-5">
            {nginx && (
              <div>
                <p className="text-xs font-bold text-gray-400 uppercase tracking-wide mb-2">NGINX</p>
                <label className="flex items-center gap-2 text-sm mb-1">
                  <input
                    type="checkbox"
                    checked={!!selectedSources['nginx_config']}
                    onChange={() => toggleSource('nginx_config')}
                  />
                  NGINX Configuration (/etc/nginx)
                </label>
                {vhosts?.map((vh, i) => (
                  <label key={i} className="flex items-center gap-2 text-sm mb-1 pl-4">
                    <input
                      type="checkbox"
                      checked={!!selectedSources[`vhost_${i}`]}
                      onChange={() => toggleSource(`vhost_${i}`)}
                    />
                    {vh.name || `VHost ${i}`} — <span className="font-mono text-xs">{vh.root_path || '(no root path)'}</span>
                  </label>
                ))}
              </div>
            )}
            {mysql && (
              <div>
                <p className="text-xs font-bold text-gray-400 uppercase tracking-wide mb-2">MySQL Databases</p>
                {databases?.map((db) => (
                  <label key={db} className="flex items-center gap-2 text-sm mb-1">
                    <input
                      type="checkbox"
                      checked={!!selectedSources[`db_${db}`]}
                      onChange={() => toggleSource(`db_${db}`)}
                    />
                    {db}
                  </label>
                ))}
              </div>
            )}
            {certbot && (
              <div>
                <p className="text-xs font-bold text-gray-400 uppercase tracking-wide mb-2">Certbot</p>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={!!selectedSources['certbot']}
                    onChange={() => toggleSource('certbot')}
                  />
                  SSL Certificates (/etc/letsencrypt)
                </label>
              </div>
            )}
            {pm2 && (
              <div>
                <p className="text-xs font-bold text-gray-400 uppercase tracking-wide mb-2">PM2</p>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={!!selectedSources['pm2']}
                    onChange={() => toggleSource('pm2')}
                  />
                  PM2 Configuration
                </label>
              </div>
            )}
            {redis && (
              <div>
                <p className="text-xs font-bold text-gray-400 uppercase tracking-wide mb-2">Redis</p>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={!!selectedSources['redis']}
                    onChange={() => toggleSource('redis')}
                  />
                  Redis Data (RDB dump) — <span className="font-mono text-xs">{(redis.data?.rdb_path as string) || '/var/lib/redis/dump.rdb'}</span>
                </label>
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  // Linux Step 4: MySQL Setup
  const renderMysqlSetup = () => {
    const createCmd = `CREATE USER '${mysqlUser}'@'localhost' IDENTIFIED BY '${mysqlPass}';
GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON *.* TO '${mysqlUser}'@'localhost';
FLUSH PRIVILEGES;`;

    return (
      <div>
        <h2 className="text-lg font-bold text-gray-900 mb-1">MySQL Backup User</h2>
        <p className="text-sm text-gray-500 mb-5">
          Create a dedicated read-only MySQL user for backups. Run the commands below on your server.
        </p>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-gray-600 mb-1">MySQL Username</label>
              <input
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                value={mysqlUser}
                onChange={(e) => setMysqlUser(e.target.value)}
              />
            </div>
            <div>
              <label className="block text-xs font-semibold text-gray-600 mb-1">MySQL Password</label>
              <input
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                value={mysqlPass}
                onChange={(e) => setMysqlPass(e.target.value)}
              />
            </div>
          </div>
          <div>
            <p className="text-xs font-semibold text-gray-600 mb-2">Run these SQL commands on your server:</p>
            <CopyBox text={createCmd} />
          </div>
          <div className="bg-amber-50 border border-amber-200 rounded-lg p-3 text-sm text-amber-800">
            Make sure to run these commands before saving, then enter the credentials above.
          </div>
        </div>
      </div>
    );
  };

  // Linux Step 5: Summary
  const renderLinuxSummary = () => {
    const sources = buildLinuxSources();
    return (
      <div>
        <h2 className="text-lg font-bold text-gray-900 mb-1">Review & Save</h2>
        <p className="text-sm text-gray-500 mb-5">Review your configuration before saving.</p>
        <div className="space-y-4">
          <div className="bg-gray-50 rounded-lg p-4 text-sm">
            <p className="font-semibold mb-2 text-gray-700">Server</p>
            <div className="grid grid-cols-2 gap-1 text-gray-600">
              <span>Name:</span><span className="font-medium">{conn.name}</span>
              <span>Host:</span><span className="font-mono">{conn.host}:{conn.port}</span>
              <span>User:</span><span className="font-mono">{conn.username}</span>
              <span>Auth:</span><span className="capitalize">{conn.authMethod}</span>
            </div>
          </div>
          <div className="bg-gray-50 rounded-lg p-4 text-sm">
            <p className="font-semibold mb-2 text-gray-700">Backup Sources ({sources.length})</p>
            {sources.length === 0 ? (
              <p className="text-gray-500">No sources selected.</p>
            ) : (
              <ul className="space-y-1">
                {sources.map((s, i) => (
                  <li key={i} className="flex items-center gap-2">
                    <span className={`text-xs px-1.5 py-0.5 rounded font-medium ${
                      s.type === 'web' ? 'bg-green-100 text-green-700' :
                      s.type === 'database' ? 'bg-purple-100 text-purple-700' :
                      'bg-gray-100 text-gray-600'
                    }`}>{s.type}</span>
                    <span>{s.name}</span>
                    <span className="text-gray-400 font-mono text-xs">{s.source_path || s.db_name}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {createServer.isError && (
            <div className="flex items-center gap-2 text-red-600 text-sm">
              <AlertCircle size={15} />
              {createServer.error?.message || 'Failed to save server'}
            </div>
          )}
        </div>
      </div>
    );
  };

  // Windows Step 1: Connection
  const renderWindowsConnection = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">FTP Connection</h2>
      <p className="text-sm text-gray-500 mb-5">Enter the FTP connection details for your Windows server.</p>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Server Name *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="My Windows Server"
              value={winConn.name}
              onChange={(e) => setWinConn({ ...winConn, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Host / IP *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="192.168.1.20"
              value={winConn.host}
              onChange={(e) => setWinConn({ ...winConn, host: e.target.value })}
            />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">FTP Port</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="21"
              value={winConn.port}
              onChange={(e) => setWinConn({ ...winConn, port: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-xs font-semibold text-gray-600 mb-1">Username *</label>
            <input
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="ftpuser"
              value={winConn.username}
              onChange={(e) => setWinConn({ ...winConn, username: e.target.value })}
            />
          </div>
        </div>
        <div>
          <label className="block text-xs font-semibold text-gray-600 mb-1">Password *</label>
          <input
            type="password"
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            value={winConn.password}
            onChange={(e) => setWinConn({ ...winConn, password: e.target.value })}
          />
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => handleTestConnection(true)}
            disabled={winTestStatus === 'loading' || !winConn.host || !winConn.username}
            className="inline-flex items-center gap-2 px-4 py-2 bg-gray-100 hover:bg-gray-200 text-gray-700 text-sm rounded-lg font-medium transition-colors disabled:opacity-50"
          >
            {winTestStatus === 'loading' && <Loader2 size={14} className="animate-spin" />}
            Test Connection
          </button>
          {winTestStatus === 'ok' && (
            <span className="flex items-center gap-1.5 text-green-600 text-sm">
              <CheckCircle2 size={15} /> Connected successfully
            </span>
          )}
          {winTestStatus === 'error' && (
            <span className="flex items-center gap-1.5 text-red-600 text-sm">
              <AlertCircle size={15} /> {winTestMsg || 'Connection failed'}
            </span>
          )}
        </div>
      </div>
    </div>
  );

  // Windows Step 2: Source Selection
  const renderWindowsSources = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">Source Selection</h2>
      <p className="text-sm text-gray-500 mb-5">Add the FTP paths you want to back up.</p>
      <div className="space-y-3">
        {manualSources.map((src, i) => (
          <div key={i} className="border border-gray-200 rounded-lg p-3 space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-gray-500">Source {i + 1}</span>
              {manualSources.length > 1 && (
                <button
                  onClick={() => setManualSources((prev) => prev.filter((_, j) => j !== i))}
                  className="text-red-400 hover:text-red-600"
                >
                  <Trash2 size={14} />
                </button>
              )}
            </div>
            <div className="grid grid-cols-3 gap-2">
              <div>
                <label className="block text-xs text-gray-500 mb-1">Name</label>
                <input
                  className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Website Files"
                  value={src.name}
                  onChange={(e) => {
                    const updated = [...manualSources];
                    updated[i] = { ...src, name: e.target.value };
                    setManualSources(updated);
                  }}
                />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">Type</label>
                <select
                  className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  value={src.type}
                  onChange={(e) => {
                    const updated = [...manualSources];
                    updated[i] = { ...src, type: e.target.value as 'web' | 'database' | 'config' };
                    setManualSources(updated);
                  }}
                >
                  <option value="web">Web</option>
                  <option value="database">Database</option>
                  <option value="config">Config</option>
                </select>
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">FTP Path</label>
                <input
                  className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="/wwwroot"
                  value={src.source_path}
                  onChange={(e) => {
                    const updated = [...manualSources];
                    updated[i] = { ...src, source_path: e.target.value };
                    setManualSources(updated);
                  }}
                />
              </div>
            </div>
          </div>
        ))}
        <button
          onClick={() =>
            setManualSources((prev) => [...prev, { name: '', type: 'web', source_path: '' }])
          }
          className="flex items-center gap-2 text-sm text-blue-600 hover:text-blue-700 font-medium"
        >
          <Plus size={15} /> Add Source
        </button>
      </div>
    </div>
  );

  // Windows Summary
  const renderWindowsSummary = () => (
    <div>
      <h2 className="text-lg font-bold text-gray-900 mb-1">Review & Save</h2>
      <p className="text-sm text-gray-500 mb-5">Review your configuration before saving.</p>
      <div className="space-y-4">
        <div className="bg-gray-50 rounded-lg p-4 text-sm">
          <p className="font-semibold mb-2 text-gray-700">Server</p>
          <div className="grid grid-cols-2 gap-1 text-gray-600">
            <span>Name:</span><span className="font-medium">{winConn.name}</span>
            <span>Host:</span><span className="font-mono">{winConn.host}:{winConn.port}</span>
            <span>User:</span><span className="font-mono">{winConn.username}</span>
          </div>
        </div>
        <div className="bg-gray-50 rounded-lg p-4 text-sm">
          <p className="font-semibold mb-2 text-gray-700">
            Backup Sources ({manualSources.filter((s) => s.name && s.source_path).length})
          </p>
          {manualSources.filter((s) => s.name && s.source_path).length === 0 ? (
            <p className="text-gray-500">No sources configured.</p>
          ) : (
            <ul className="space-y-1">
              {manualSources.filter((s) => s.name && s.source_path).map((s, i) => (
                <li key={i} className="flex items-center gap-2">
                  <span className="text-xs px-1.5 py-0.5 rounded font-medium bg-blue-100 text-blue-700">{s.type}</span>
                  <span>{s.name}</span>
                  <span className="text-gray-400 font-mono text-xs">{s.source_path}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
        {createServer.isError && (
          <div className="flex items-center gap-2 text-red-600 text-sm">
            <AlertCircle size={15} />
            {createServer.error?.message || 'Failed to save server'}
          </div>
        )}
      </div>
    </div>
  );

  // ── Navigation logic ───────────────────────────────────────────────────────

  const linuxStepCount = LINUX_STEPS.length;
  const winStepCount = WIN_STEPS.length;

  const getLinuxStepContent = () => {
    if (step === 0) return renderStep0();
    if (step === 1) return renderLinuxConnection();
    if (step === 2) return renderDiscovery();
    if (step === 3) return renderSourceSelection();
    if (step === 4) return renderMysqlSetup();
    if (step === 5) return renderLinuxSummary();
    return null;
  };

  const getWindowsStepContent = () => {
    if (step === 0) return renderStep0();
    if (step === 1) return renderWindowsConnection();
    if (step === 2) return renderWindowsSources();
    if (step === 3) return renderWindowsSummary();
    return null;
  };

  const isLastStep =
    (serverType === 'linux' && step === linuxStepCount - 1) ||
    (serverType === 'windows' && step === winStepCount - 1) ||
    (!serverType && step === 0);

  const canNext = () => {
    if (step === 0) return false; // type selection navigates directly
    if (serverType === 'linux' && step === 1) return !!conn.name && !!conn.host && !!conn.username;
    if (serverType === 'windows' && step === 1) return !!winConn.name && !!winConn.host && !!winConn.username;
    return true;
  };

  const handleNext = () => {
    if (serverType === 'linux') {
      // Skip mysql step if no databases selected
      if (step === 3 && !hasDatabases()) {
        setStep(5); // skip to summary
        return;
      }
    }
    setStep((s) => s + 1);
  };

  const handleBack = () => {
    if (step === 5 && serverType === 'linux' && !hasDatabases()) {
      setStep(3); // skip back over mysql
      return;
    }
    if (step === 1) {
      setServerType(null);
      setStep(0);
      return;
    }
    setStep((s) => s - 1);
  };

  const currentSteps = serverType === 'windows' ? WIN_STEPS : LINUX_STEPS;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-2xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 pt-6 pb-4 border-b border-gray-100">
          <h1 className="text-lg font-bold text-gray-900">Add Server</h1>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-700 transition-colors"
          >
            <X size={20} />
          </button>
        </div>

        {/* Stepper */}
        <div className="px-6 pt-5">
          {serverType && (
            <Stepper steps={currentSteps} current={step} />
          )}
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-6 pb-4">
          {serverType === 'linux'
            ? getLinuxStepContent()
            : serverType === 'windows'
            ? getWindowsStepContent()
            : renderStep0()}
        </div>

        {/* Footer */}
        {step > 0 && (
          <div className="flex items-center justify-between px-6 py-4 border-t border-gray-100">
            <button
              onClick={handleBack}
              className="flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-800 font-medium transition-colors"
            >
              <ChevronLeft size={16} /> Back
            </button>
            {isLastStep ? (
              <button
                onClick={() => createServer.mutate()}
                disabled={createServer.isPending}
                className="flex items-center gap-2 px-5 py-2 bg-blue-600 text-white text-sm rounded-lg font-semibold hover:bg-blue-700 transition-colors disabled:opacity-60"
              >
                {createServer.isPending && <Loader2 size={14} className="animate-spin" />}
                Save Server
              </button>
            ) : (
              <button
                onClick={handleNext}
                disabled={!canNext()}
                className="flex items-center gap-1.5 px-5 py-2 bg-blue-600 text-white text-sm rounded-lg font-semibold hover:bg-blue-700 transition-colors disabled:opacity-50"
              >
                Next <ChevronRight size={16} />
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
