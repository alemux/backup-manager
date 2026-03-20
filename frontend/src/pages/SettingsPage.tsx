import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Bell,
  HardDrive,
  Lock,
  Users,
  Settings,
  Plus,
  Trash2,
  Edit2,
  Check,
  X,
  Eye,
  EyeOff,
  KeyRound,
  Shield,
} from 'lucide-react';
import { settingsApi, type Destination, type NotificationConfig, type User } from '../api/settings';

// ─── Tab definitions ────────────────────────────────────────────────────────

type TabId = 'notifications' | 'destinations' | 'encryption' | 'users' | 'general';

const TABS: { id: TabId; label: string; icon: React.ReactNode }[] = [
  { id: 'notifications', label: 'Notifications', icon: <Bell className="w-4 h-4" /> },
  { id: 'destinations', label: 'Destinations', icon: <HardDrive className="w-4 h-4" /> },
  { id: 'encryption', label: 'Encryption', icon: <Lock className="w-4 h-4" /> },
  { id: 'users', label: 'Users', icon: <Users className="w-4 h-4" /> },
  { id: 'general', label: 'General', icon: <Settings className="w-4 h-4" /> },
];

// ─── Shared UI ───────────────────────────────────────────────────────────────

function SectionCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-6">
      <h3 className="text-base font-semibold text-gray-800 mb-4">{title}</h3>
      {children}
    </div>
  );
}

function Input({
  label,
  type = 'text',
  value,
  onChange,
  placeholder,
  className = '',
}: {
  label?: string;
  type?: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <div className={className}>
      {label && <label className="block text-sm font-medium text-gray-700 mb-1">{label}</label>}
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      />
    </div>
  );
}

function Badge({ children, color = 'gray' }: { children: React.ReactNode; color?: string }) {
  const colors: Record<string, string> = {
    gray: 'bg-gray-100 text-gray-700',
    blue: 'bg-blue-100 text-blue-700',
    green: 'bg-green-100 text-green-700',
    purple: 'bg-purple-100 text-purple-700',
    orange: 'bg-orange-100 text-orange-700',
    red: 'bg-red-100 text-red-700',
  };
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${colors[color] || colors.gray}`}>
      {children}
    </span>
  );
}

// ─── Notifications Tab ───────────────────────────────────────────────────────

const EVENT_TYPES = [
  'backup_started',
  'backup_success',
  'backup_failed',
  'backup_timeout',
  'server_offline',
  'disk_warning',
  'integrity_failed',
];

function NotificationsTab() {
  const qc = useQueryClient();

  const { data: configs = [], isLoading } = useQuery({
    queryKey: ['notification-configs'],
    queryFn: settingsApi.getNotifications,
  });

  // Load saved connection settings from the settings table
  const { data: settings = {} } = useQuery({
    queryKey: ['settings'],
    queryFn: settingsApi.getSettings,
  });

  const [telegramToken, setTelegramToken] = useState('');
  const [telegramChatId, setTelegramChatId] = useState('');
  const [emailHost, setEmailHost] = useState('');
  const [emailPort, setEmailPort] = useState('587');
  const [emailUser, setEmailUser] = useState('');
  const [emailPass, setEmailPass] = useState('');
  const [emailFrom, setEmailFrom] = useState('');
  const [emailTestTo, setEmailTestTo] = useState('');
  const [settingsLoaded, setSettingsLoaded] = useState(false);

  // Populate fields from saved settings on first load
  if (Object.keys(settings).length > 0 && !settingsLoaded) {
    if (settings['telegram_bot_token']) setTelegramToken(settings['telegram_bot_token']);
    if (settings['telegram_chat_id']) setTelegramChatId(settings['telegram_chat_id']);
    if (settings['smtp_host']) setEmailHost(settings['smtp_host']);
    if (settings['smtp_port']) setEmailPort(settings['smtp_port']);
    if (settings['smtp_user']) setEmailUser(settings['smtp_user']);
    if (settings['smtp_pass']) setEmailPass(settings['smtp_pass']);
    if (settings['smtp_from']) setEmailFrom(settings['smtp_from']);
    setSettingsLoaded(true);
  }

  const [localConfigs, setLocalConfigs] = useState<Record<string, NotificationConfig>>({});
  const [testResult, setTestResult] = useState<string | null>(null);
  const [testError, setTestError] = useState<string | null>(null);

  // Merge server configs into local state on first load
  const effectiveConfigs = (configs.length > 0 ? configs : EVENT_TYPES.map((et) => ({
    id: 0,
    event_type: et,
    telegram_enabled: false,
    email_enabled: false,
    telegram_chat_id: '',
    email_recipients: '',
  }))).map((c) => localConfigs[c.event_type] ?? c);

  const saveMutation = useMutation({
    mutationFn: async () => {
      // Save connection settings to the settings table
      await settingsApi.updateSettings({
        telegram_bot_token: telegramToken,
        telegram_chat_id: telegramChatId,
        smtp_host: emailHost,
        smtp_port: emailPort,
        smtp_user: emailUser,
        smtp_pass: emailPass,
        smtp_from: emailFrom,
      });
      // Save per-event notification toggles
      return settingsApi.updateNotifications(effectiveConfigs);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notification-configs'] });
      qc.invalidateQueries({ queryKey: ['settings'] });
    },
  });

  const testMutation = useMutation({
    mutationFn: (payload: Record<string, unknown>) =>
      settingsApi.testNotification(payload),
    onSuccess: () => {
      setTestResult('Test notification sent successfully!');
      setTestError(null);
    },
    onError: (err: Error) => {
      setTestError(err.message);
      setTestResult(null);
    },
  });

  function toggleCheckbox(eventType: string, field: 'telegram_enabled' | 'email_enabled') {
    const base = effectiveConfigs.find((c) => c.event_type === eventType);
    if (!base) return;
    setLocalConfigs((prev) => ({
      ...prev,
      [eventType]: { ...base, [field]: !base[field] },
    }));
  }

  return (
    <div className="space-y-6">
      {/* Telegram */}
      <SectionCard title="Telegram">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
          <Input label="Bot Token" value={telegramToken} onChange={setTelegramToken} placeholder="123456:ABC-..." />
          <Input label="Chat ID" value={telegramChatId} onChange={setTelegramChatId} placeholder="-100123456789" />
        </div>
        <button
          onClick={() => testMutation.mutate({ channel: 'telegram', target: telegramChatId, bot_token: telegramToken })}
          disabled={!telegramChatId || !telegramToken || testMutation.isPending}
          className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
        >
          {testMutation.isPending ? 'Sending…' : 'Send Test Message'}
        </button>
        {testResult && <p className="mt-3 text-sm text-green-600">{testResult}</p>}
        {testError && <p className="mt-3 text-sm text-red-600">{testError}</p>}
      </SectionCard>

      {/* Email */}
      <SectionCard title="Email (SMTP)">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
          <Input label="SMTP Host" value={emailHost} onChange={setEmailHost} placeholder="smtp.example.com" />
          <Input label="Port" value={emailPort} onChange={setEmailPort} placeholder="587" />
          <Input label="Username" value={emailUser} onChange={setEmailUser} placeholder="user@example.com" />
          <Input label="Password" type="password" value={emailPass} onChange={setEmailPass} placeholder="••••••••" />
          <Input label="From Address" value={emailFrom} onChange={setEmailFrom} placeholder="backups@example.com" />
          <Input label="Test Recipient" value={emailTestTo} onChange={setEmailTestTo} placeholder="you@example.com" />
        </div>
        <button
          onClick={() => testMutation.mutate({ channel: 'email', target: emailTestTo, smtp_host: emailHost, smtp_port: parseInt(emailPort) || 587, smtp_user: emailUser, smtp_pass: emailPass, smtp_from: emailFrom })}
          disabled={!emailTestTo || !emailHost || !emailFrom || testMutation.isPending}
          className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
        >
          {testMutation.isPending ? 'Sending…' : 'Send Test Email'}
        </button>
      </SectionCard>

      {/* Event toggles */}
      <SectionCard title="Per-Event Notification Settings">
        {isLoading ? (
          <div className="flex justify-center py-6">
            <div className="w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100">
                  <th className="text-left py-2 pr-4 font-semibold text-gray-700">Event</th>
                  <th className="text-center py-2 px-4 font-semibold text-gray-700">Telegram</th>
                  <th className="text-center py-2 px-4 font-semibold text-gray-700">Email</th>
                </tr>
              </thead>
              <tbody>
                {effectiveConfigs.map((cfg) => (
                  <tr key={cfg.event_type} className="border-b border-gray-50 hover:bg-gray-50">
                    <td className="py-2.5 pr-4 text-gray-700 font-mono text-xs">{cfg.event_type}</td>
                    <td className="py-2.5 px-4 text-center">
                      <input
                        type="checkbox"
                        checked={cfg.telegram_enabled}
                        onChange={() => toggleCheckbox(cfg.event_type, 'telegram_enabled')}
                        className="w-4 h-4 accent-blue-600"
                      />
                    </td>
                    <td className="py-2.5 px-4 text-center">
                      <input
                        type="checkbox"
                        checked={cfg.email_enabled}
                        onChange={() => toggleCheckbox(cfg.event_type, 'email_enabled')}
                        className="w-4 h-4 accent-blue-600"
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="mt-4 flex items-center gap-3">
          <button
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending}
            className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            {saveMutation.isPending ? 'Saving…' : 'Save Changes'}
          </button>
          {saveMutation.isSuccess && (
            <span className="text-sm text-green-600 flex items-center gap-1">
              <Check className="w-4 h-4" /> Saved
            </span>
          )}
          {saveMutation.isError && (
            <span className="text-sm text-red-600">{(saveMutation.error as Error).message}</span>
          )}
        </div>
      </SectionCard>
    </div>
  );
}

// ─── Destinations Tab ────────────────────────────────────────────────────────

const DEST_TYPE_COLORS: Record<string, string> = {
  local: 'blue',
  nas: 'purple',
  usb: 'orange',
  s3: 'green',
};

function DestinationsTab() {
  const qc = useQueryClient();

  const { data: destinations = [], isLoading } = useQuery({
    queryKey: ['destinations'],
    queryFn: settingsApi.getDestinations,
  });

  const primaryDest = destinations.find((d) => d.is_primary);
  const secondaryDests = destinations.filter((d) => !d.is_primary);

  const [showForm, setShowForm] = useState(false);
  const [editId, setEditId] = useState<number | null>(null);
  const [formIsPrimary, setFormIsPrimary] = useState(false);
  const [form, setForm] = useState({
    name: '',
    type: 'local' as Destination['type'],
    path: '',
    retention_daily: '7',
    retention_weekly: '4',
    retention_monthly: '3',
    is_primary: false,
    enabled: true,
  });

  function resetForm() {
    setForm({ name: '', type: 'local', path: '', retention_daily: '7', retention_weekly: '4', retention_monthly: '3', is_primary: false, enabled: true });
    setEditId(null);
    setShowForm(false);
    setFormIsPrimary(false);
  }

  function startEdit(dest: Destination) {
    setForm({
      name: dest.name,
      type: dest.type,
      path: dest.path,
      retention_daily: String(dest.retention_daily),
      retention_weekly: String(dest.retention_weekly),
      retention_monthly: String(dest.retention_monthly),
      is_primary: dest.is_primary,
      enabled: dest.enabled,
    });
    setEditId(dest.id);
    setFormIsPrimary(dest.is_primary);
    setShowForm(true);
  }

  function startAddPrimary() {
    resetForm();
    setForm((f) => ({ ...f, name: 'Primary Backup Storage', is_primary: true }));
    setFormIsPrimary(true);
    setShowForm(true);
  }

  function startAddSecondary() {
    resetForm();
    setShowForm(true);
  }

  const createMutation = useMutation({
    mutationFn: (data: Partial<Destination>) => settingsApi.createDestination(data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['destinations'] }); resetForm(); },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: Partial<Destination> }) =>
      settingsApi.updateDestination(id, data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['destinations'] }); resetForm(); },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => settingsApi.deleteDestination(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['destinations'] }),
  });

  function handleSubmit() {
    const payload: Partial<Destination> = {
      name: form.name,
      type: form.type,
      path: form.path,
      retention_daily: Number(form.retention_daily),
      retention_weekly: Number(form.retention_weekly),
      retention_monthly: Number(form.retention_monthly),
      is_primary: formIsPrimary || form.is_primary,
      enabled: form.enabled,
    };
    if (editId !== null) {
      updateMutation.mutate({ id: editId, data: payload });
    } else {
      createMutation.mutate(payload);
    }
  }

  const isSaving = createMutation.isPending || updateMutation.isPending;
  const saveError = (createMutation.error || updateMutation.error) as Error | null;

  return (
    <div className="space-y-6">
      {/* Explanation */}
      <div className="bg-blue-50 border border-blue-200 rounded-xl p-4 text-sm text-blue-800">
        <p className="font-semibold mb-1">Come funziona lo storage dei backup</p>
        <p>
          La <strong>Destinazione Primaria</strong> è dove BackupManager salva i backup dai tuoi server.
          Deve essere un disco con spazio sufficiente (es. un disco dedicato montato su <code className="bg-blue-100 px-1 rounded">/mnt/backup</code>).
        </p>
        <p className="mt-1">
          Le <strong>Destinazioni Secondarie</strong> sono copie aggiuntive: un NAS in rete, un disco USB esterno, o un cloud storage.
          BackupManager copia automaticamente ogni backup dalla primaria alle secondarie dopo ogni esecuzione.
        </p>
      </div>

      {isLoading ? (
        <div className="flex justify-center py-10">
          <div className="w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
        </div>
      ) : (
        <>
          {/* ─── Primary Destination ─── */}
          <SectionCard title="Destinazione Primaria">
            {primaryDest ? (
              <div className="flex items-center gap-4">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-semibold text-gray-800">{primaryDest.name}</span>
                    <Badge color="green">Primaria</Badge>
                    <Badge color={DEST_TYPE_COLORS[primaryDest.type] || 'gray'}>{primaryDest.type.toUpperCase()}</Badge>
                  </div>
                  <p className="text-sm text-gray-600 mt-1 font-mono">{primaryDest.path}</p>
                  <p className="text-xs text-gray-400 mt-0.5">
                    Retention: {primaryDest.retention_daily} giorni / {primaryDest.retention_weekly} settimane / {primaryDest.retention_monthly} mesi
                  </p>
                </div>
                <button
                  onClick={() => startEdit(primaryDest)}
                  className="px-3 py-1.5 text-sm font-medium text-blue-600 border border-blue-200 rounded-lg hover:bg-blue-50"
                >
                  Modifica
                </button>
              </div>
            ) : (
              <div className="text-center py-6">
                <HardDrive className="w-8 h-8 mx-auto mb-2 text-amber-400" />
                <p className="text-sm text-gray-600 mb-1">Nessuna destinazione primaria configurata</p>
                <p className="text-xs text-gray-400 mb-3">I backup vengono attualmente salvati nella cartella di default del programma.</p>
                <button
                  onClick={startAddPrimary}
                  className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700"
                >
                  <Plus className="w-4 h-4" /> Configura Destinazione Primaria
                </button>
              </div>
            )}
          </SectionCard>

          {/* ─── Secondary Destinations ─── */}
          <SectionCard title="Destinazioni Secondarie (copie aggiuntive)">
            <p className="text-xs text-gray-500 mb-4">
              Dopo ogni backup, i dati vengono copiati automaticamente su queste destinazioni.
              Ogni destinazione ha la sua policy di retention indipendente.
            </p>

            {secondaryDests.length === 0 ? (
              <div className="text-center py-6 border border-dashed border-gray-200 rounded-lg">
                <p className="text-sm text-gray-400 mb-2">Nessuna destinazione secondaria.</p>
                <p className="text-xs text-gray-400 mb-3">Aggiungi un NAS, un disco USB o un cloud storage per avere copie ridondanti.</p>
              </div>
            ) : (
              <div className="space-y-3 mb-4">
                {secondaryDests.map((dest) => (
                  <div key={dest.id} className="bg-gray-50 rounded-lg border border-gray-100 p-3 flex items-center gap-4">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-semibold text-gray-800 text-sm">{dest.name}</span>
                        <Badge color={DEST_TYPE_COLORS[dest.type] || 'gray'}>{dest.type.toUpperCase()}</Badge>
                        {!dest.enabled && <Badge color="gray">Disabilitata</Badge>}
                      </div>
                      <p className="text-xs text-gray-500 mt-0.5 font-mono truncate">{dest.path}</p>
                      <p className="text-xs text-gray-400">
                        Retention: {dest.retention_daily}g / {dest.retention_weekly}s / {dest.retention_monthly}m
                      </p>
                    </div>
                    <div className="flex items-center gap-1">
                      <button onClick={() => startEdit(dest)} className="p-1.5 rounded-lg text-gray-400 hover:text-blue-600 hover:bg-blue-50">
                        <Edit2 className="w-4 h-4" />
                      </button>
                      <button onClick={() => { if (confirm('Eliminare questa destinazione?')) deleteMutation.mutate(dest.id); }} className="p-1.5 rounded-lg text-gray-400 hover:text-red-600 hover:bg-red-50">
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}

            <button
              onClick={startAddSecondary}
              className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700"
            >
              <Plus className="w-4 h-4" /> Aggiungi Destinazione Secondaria
            </button>
          </SectionCard>
        </>
      )}

      {/* ─── Add/Edit Form ─── */}
      {showForm && (
        <div className="bg-white rounded-xl border border-blue-200 shadow-sm p-6">
          <h3 className="text-base font-semibold text-gray-800 mb-4">
            {editId !== null ? 'Modifica Destinazione' : formIsPrimary ? 'Configura Destinazione Primaria' : 'Aggiungi Destinazione Secondaria'}
          </h3>
          {formIsPrimary && (
            <div className="bg-amber-50 border border-amber-200 rounded-lg p-3 text-sm text-amber-800 mb-4">
              Scegli un percorso su un disco con molto spazio disponibile. Tutti i backup verranno salvati qui.
              Esempio: <code className="bg-amber-100 px-1 rounded">/mnt/backup</code> oppure <code className="bg-amber-100 px-1 rounded">/media/backup-disk</code>
            </div>
          )}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Input label="Nome" value={form.name} onChange={(v) => setForm((f) => ({ ...f, name: v }))} placeholder={formIsPrimary ? "Disco Backup Principale" : "NAS Ufficio"} />
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Tipo</label>
              <select
                value={form.type}
                onChange={(e) => setForm((f) => ({ ...f, type: e.target.value as Destination['type'] }))}
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                {(formIsPrimary ? ['local'] : ['local', 'nas', 'usb', 's3']).map((t) => (
                  <option key={t} value={t}>{t === 'local' ? 'Disco Locale' : t === 'nas' ? 'NAS (rete)' : t === 'usb' ? 'Disco USB' : 'Cloud (S3)'}</option>
                ))}
              </select>
            </div>
            <Input label="Percorso" value={form.path} onChange={(v) => setForm((f) => ({ ...f, path: v }))} placeholder={formIsPrimary ? "/mnt/backup" : "/mnt/nas/backups"} className="sm:col-span-2" />
            <Input label="Retention giornaliera (giorni)" value={form.retention_daily} onChange={(v) => setForm((f) => ({ ...f, retention_daily: v }))} />
            <Input label="Retention settimanale (settimane)" value={form.retention_weekly} onChange={(v) => setForm((f) => ({ ...f, retention_weekly: v }))} />
            <Input label="Retention mensile (mesi)" value={form.retention_monthly} onChange={(v) => setForm((f) => ({ ...f, retention_monthly: v }))} />
            {!formIsPrimary && (
              <div className="flex items-center">
                <label className="flex items-center gap-2 text-sm text-gray-700 cursor-pointer">
                  <input type="checkbox" checked={form.enabled} onChange={(e) => setForm((f) => ({ ...f, enabled: e.target.checked }))} className="w-4 h-4 accent-blue-600" />
                  Abilitata
                </label>
              </div>
            )}
          </div>

          {saveError && <p className="mt-3 text-sm text-red-600">{saveError.message}</p>}

          <div className="mt-4 flex items-center gap-3">
            <button
              onClick={handleSubmit}
              disabled={isSaving}
              className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {isSaving ? 'Salvataggio…' : (editId !== null ? 'Aggiorna' : 'Salva')}
            </button>
            <button
              onClick={resetForm}
              className="px-4 py-2 text-sm font-medium text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50"
            >
              Annulla
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Encryption Tab ──────────────────────────────────────────────────────────

function EncryptionTab() {
  const qc = useQueryClient();

  const { data: settings = {} } = useQuery({
    queryKey: ['settings'],
    queryFn: settingsApi.getSettings,
  });

  const [generatedKey, setGeneratedKey] = useState<string | null>(null);
  const [showKey, setShowKey] = useState(false);

  const encryptionEnabled = settings['encryption_enabled'] === 'true';

  const toggleMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      settingsApi.updateSettings({ encryption_enabled: String(enabled) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  });

  function generateKey() {
    const arr = new Uint8Array(32);
    crypto.getRandomValues(arr);
    const key = Array.from(arr).map((b) => b.toString(16).padStart(2, '0')).join('');
    setGeneratedKey(key);
    setShowKey(true);
  }

  function downloadKey() {
    if (!generatedKey) return;
    const blob = new Blob([generatedKey], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'backup-encryption.key';
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="space-y-6">
      <SectionCard title="Encryption Settings">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-gray-800">Enable Encryption</p>
            <p className="text-xs text-gray-500 mt-0.5">Encrypt backup snapshots at rest using AES-256.</p>
          </div>
          <button
            onClick={() => toggleMutation.mutate(!encryptionEnabled)}
            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${encryptionEnabled ? 'bg-blue-600' : 'bg-gray-200'}`}
          >
            <span
              className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${encryptionEnabled ? 'translate-x-6' : 'translate-x-1'}`}
            />
          </button>
        </div>
        {toggleMutation.isError && (
          <p className="mt-2 text-sm text-red-600">{(toggleMutation.error as Error).message}</p>
        )}
      </SectionCard>

      <SectionCard title="Encryption Key">
        <div className="space-y-4">
          <div className="bg-amber-50 border border-amber-200 rounded-lg p-3 text-sm text-amber-800">
            <Shield className="w-4 h-4 inline mr-1" />
            Store your encryption key in a safe place. If you lose it, encrypted backups cannot be recovered.
          </div>

          <button
            onClick={generateKey}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium bg-indigo-600 text-white rounded-lg hover:bg-indigo-700"
          >
            <KeyRound className="w-4 h-4" /> Generate New Key
          </button>

          {generatedKey && (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <code className="flex-1 bg-gray-100 rounded-lg px-3 py-2 text-xs font-mono break-all">
                  {showKey ? generatedKey : '••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••'}
                </code>
                <button
                  onClick={() => setShowKey((v) => !v)}
                  className="p-2 text-gray-400 hover:text-gray-700"
                >
                  {showKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
              <button
                onClick={downloadKey}
                className="text-sm text-blue-600 hover:underline"
              >
                Download key file
              </button>
            </div>
          )}
        </div>
      </SectionCard>
    </div>
  );
}

// ─── Users Tab ───────────────────────────────────────────────────────────────

function UsersTab() {
  const qc = useQueryClient();

  const { data: users = [], isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: settingsApi.listUsers,
  });

  const [showForm, setShowForm] = useState(false);
  const [pwdUserId, setPwdUserId] = useState<number | null>(null);
  const [newPassword, setNewPassword] = useState('');
  const [showPwd, setShowPwd] = useState(false);
  const [form, setForm] = useState({ username: '', email: '', password: '', is_admin: false });
  const [formError, setFormError] = useState<string | null>(null);

  function resetForm() {
    setForm({ username: '', email: '', password: '', is_admin: false });
    setShowForm(false);
    setFormError(null);
  }

  const createMutation = useMutation({
    mutationFn: (data: { username: string; email: string; password: string; is_admin: boolean }) =>
      settingsApi.createUser(data),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['users'] }); resetForm(); },
    onError: (err: Error) => setFormError(err.message),
  });

  const updatePwdMutation = useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) =>
      settingsApi.updateUser(id, { password }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['users'] }); setPwdUserId(null); setNewPassword(''); },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => settingsApi.deleteUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  });

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <button
          onClick={() => { resetForm(); setShowForm(true); }}
          className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700"
        >
          <Plus className="w-4 h-4" /> Add User
        </button>
      </div>

      {isLoading ? (
        <div className="flex justify-center py-10">
          <div className="w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
        </div>
      ) : (
        <div className="bg-white rounded-xl border border-gray-100 shadow-sm overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-100 bg-gray-50">
                <th className="text-left py-3 px-4 font-semibold text-gray-600">Username</th>
                <th className="text-left py-3 px-4 font-semibold text-gray-600">Email</th>
                <th className="text-left py-3 px-4 font-semibold text-gray-600">Role</th>
                <th className="text-right py-3 px-4 font-semibold text-gray-600">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user: User) => (
                <tr key={user.id} className="border-b border-gray-50 hover:bg-gray-50">
                  <td className="py-3 px-4 font-medium text-gray-800">{user.username}</td>
                  <td className="py-3 px-4 text-gray-600">{user.email}</td>
                  <td className="py-3 px-4">
                    {user.is_admin ? <Badge color="purple">Admin</Badge> : <Badge color="gray">User</Badge>}
                  </td>
                  <td className="py-3 px-4">
                    <div className="flex items-center justify-end gap-2">
                      {pwdUserId === user.id ? (
                        <div className="flex items-center gap-2">
                          <div className="relative">
                            <input
                              type={showPwd ? 'text' : 'password'}
                              value={newPassword}
                              onChange={(e) => setNewPassword(e.target.value)}
                              placeholder="New password"
                              className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm w-40 focus:outline-none focus:ring-2 focus:ring-blue-500"
                            />
                            <button
                              onClick={() => setShowPwd((v) => !v)}
                              className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400"
                            >
                              {showPwd ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                            </button>
                          </div>
                          <button
                            onClick={() => updatePwdMutation.mutate({ id: user.id, password: newPassword })}
                            disabled={!newPassword || updatePwdMutation.isPending}
                            className="p-1.5 rounded-lg text-green-600 hover:bg-green-50"
                          >
                            <Check className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => { setPwdUserId(null); setNewPassword(''); }}
                            className="p-1.5 rounded-lg text-gray-400 hover:bg-gray-100"
                          >
                            <X className="w-4 h-4" />
                          </button>
                        </div>
                      ) : (
                        <button
                          onClick={() => { setPwdUserId(user.id); setNewPassword(''); }}
                          className="text-xs px-2.5 py-1 rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-50"
                        >
                          Change Password
                        </button>
                      )}
                      <button
                        onClick={() => { if (confirm(`Delete user "${user.username}"?`)) deleteMutation.mutate(user.id); }}
                        className="p-1.5 rounded-lg text-gray-400 hover:text-red-600 hover:bg-red-50"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showForm && (
        <div className="bg-white rounded-xl border border-blue-200 shadow-sm p-6">
          <h3 className="text-base font-semibold text-gray-800 mb-4">Add User</h3>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Input label="Username" value={form.username} onChange={(v) => setForm((f) => ({ ...f, username: v }))} />
            <Input label="Email" type="email" value={form.email} onChange={(v) => setForm((f) => ({ ...f, email: v }))} />
            <Input label="Password" type="password" value={form.password} onChange={(v) => setForm((f) => ({ ...f, password: v }))} />
            <div className="flex items-center gap-2 pt-6">
              <label className="flex items-center gap-2 text-sm text-gray-700 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.is_admin}
                  onChange={(e) => setForm((f) => ({ ...f, is_admin: e.target.checked }))}
                  className="w-4 h-4 accent-blue-600"
                />
                Admin
              </label>
            </div>
          </div>
          {formError && <p className="mt-3 text-sm text-red-600">{formError}</p>}
          <div className="mt-4 flex items-center gap-3">
            <button
              onClick={() => createMutation.mutate(form)}
              disabled={createMutation.isPending}
              className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {createMutation.isPending ? 'Creating…' : 'Create User'}
            </button>
            <button onClick={resetForm} className="px-4 py-2 text-sm font-medium text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50">
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── General Tab ─────────────────────────────────────────────────────────────

const TIMEZONES = ['UTC', 'America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Asia/Tokyo', 'Asia/Shanghai', 'Australia/Sydney'];
const AI_PROVIDERS = ['OpenAI', 'Anthropic'];
const OPENAI_MODELS = ['gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo', 'gpt-3.5-turbo'];
const ANTHROPIC_MODELS = ['claude-opus-4-5', 'claude-sonnet-4-5', 'claude-haiku-4-5'];

function GeneralTab() {
  const qc = useQueryClient();

  const { data: settings = {}, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: settingsApi.getSettings,
  });

  const [timezone, setTimezone] = useState('');
  const [retentionDaily, setRetentionDaily] = useState('');
  const [retentionWeekly, setRetentionWeekly] = useState('');
  const [retentionMonthly, setRetentionMonthly] = useState('');
  const [bandwidthLimit, setBandwidthLimit] = useState('');
  const [aiProvider, setAiProvider] = useState('');
  const [aiKey, setAiKey] = useState('');
  const [aiModel, setAiModel] = useState('');
  const [globalExcludePatterns, setGlobalExcludePatterns] = useState('');
  const [initialized, setInitialized] = useState(false);

  // Initialize local state once settings load
  if (!isLoading && !initialized && Object.keys(settings).length >= 0) {
    setTimezone(settings['timezone'] || 'UTC');
    setRetentionDaily(settings['default_retention_daily'] || '7');
    setRetentionWeekly(settings['default_retention_weekly'] || '4');
    setRetentionMonthly(settings['default_retention_monthly'] || '3');
    setBandwidthLimit(settings['bandwidth_limit_mbps'] || '');
    setAiProvider(settings['ai_provider'] || 'OpenAI');
    setAiKey(settings['ai_api_key'] || '');
    setAiModel(settings['ai_model'] || '');
    setGlobalExcludePatterns(settings['global_exclude_patterns'] || '');
    setInitialized(true);
  }

  const saveMutation = useMutation({
    mutationFn: () =>
      settingsApi.updateSettings({
        timezone,
        default_retention_daily: retentionDaily,
        default_retention_weekly: retentionWeekly,
        default_retention_monthly: retentionMonthly,
        bandwidth_limit_mbps: bandwidthLimit,
        ai_provider: aiProvider,
        ai_api_key: aiKey,
        ai_model: aiModel,
        global_exclude_patterns: globalExcludePatterns,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  });

  const models = aiProvider === 'Anthropic' ? ANTHROPIC_MODELS : OPENAI_MODELS;

  return (
    <div className="space-y-6">
      <SectionCard title="Time &amp; Locale">
        <div className="max-w-xs">
          <label className="block text-sm font-medium text-gray-700 mb-1">Timezone</label>
          <select
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {TIMEZONES.map((tz) => (
              <option key={tz} value={tz}>{tz}</option>
            ))}
          </select>
        </div>
      </SectionCard>

      <SectionCard title="Default Retention Policy">
        <div className="grid grid-cols-3 gap-4 max-w-md">
          <Input label="Daily" value={retentionDaily} onChange={setRetentionDaily} placeholder="7" />
          <Input label="Weekly" value={retentionWeekly} onChange={setRetentionWeekly} placeholder="4" />
          <Input label="Monthly" value={retentionMonthly} onChange={setRetentionMonthly} placeholder="3" />
        </div>
        <p className="text-xs text-gray-400 mt-2">Number of snapshots to keep per interval.</p>
      </SectionCard>

      <SectionCard title="Bandwidth Limits">
        <div className="max-w-xs">
          <Input
            label="Global Bandwidth Limit (Mbps)"
            value={bandwidthLimit}
            onChange={setBandwidthLimit}
            placeholder="Leave empty for unlimited"
          />
        </div>
      </SectionCard>

      <SectionCard title="AI Assistant">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Provider</label>
            <select
              value={aiProvider}
              onChange={(e) => { setAiProvider(e.target.value); setAiModel(''); }}
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              {AI_PROVIDERS.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Model</label>
            <select
              value={aiModel}
              onChange={(e) => setAiModel(e.target.value)}
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="">Select model…</option>
              {models.map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </div>
          <Input
            label="API Key"
            type="password"
            value={aiKey}
            onChange={setAiKey}
            placeholder="sk-..."
            className="sm:col-span-2"
          />
        </div>
      </SectionCard>

      <SectionCard title="Global Exclude Patterns">
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">Exclude Patterns</label>
          <textarea
            value={globalExcludePatterns}
            onChange={(e) => setGlobalExcludePatterns(e.target.value)}
            placeholder="node_modules&#10;.git&#10;*.log"
            rows={5}
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
          />
          <p className="text-xs text-gray-400 mt-1">
            These patterns are excluded from all backup sources (one per line or comma-separated). Per-source patterns are applied in addition to these.
          </p>
        </div>
      </SectionCard>

      <div className="flex items-center gap-3">
        <button
          onClick={() => saveMutation.mutate()}
          disabled={saveMutation.isPending}
          className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
        >
          {saveMutation.isPending ? 'Saving…' : 'Save General Settings'}
        </button>
        {saveMutation.isSuccess && (
          <span className="text-sm text-green-600 flex items-center gap-1">
            <Check className="w-4 h-4" /> Saved
          </span>
        )}
        {saveMutation.isError && (
          <span className="text-sm text-red-600">{(saveMutation.error as Error).message}</span>
        )}
      </div>
    </div>
  );
}

// ─── Page ────────────────────────────────────────────────────────────────────

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState<TabId>('notifications');

  return (
    <div className="p-6 max-w-5xl mx-auto">
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
        <p className="text-sm text-gray-500 mt-0.5">Manage notifications, destinations, encryption, users, and general configuration.</p>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-gray-100 rounded-xl p-1 mb-6 flex-wrap">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`flex items-center gap-1.5 px-4 py-2 rounded-lg text-sm font-medium transition-all ${
              activeTab === tab.id
                ? 'bg-white text-blue-700 shadow-sm'
                : 'text-gray-500 hover:text-gray-700'
            }`}
          >
            {tab.icon}
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {activeTab === 'notifications' && <NotificationsTab />}
      {activeTab === 'destinations' && <DestinationsTab />}
      {activeTab === 'encryption' && <EncryptionTab />}
      {activeTab === 'users' && <UsersTab />}
      {activeTab === 'general' && <GeneralTab />}
    </div>
  );
}
