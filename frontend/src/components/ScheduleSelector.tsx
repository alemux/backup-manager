import { useState, useEffect } from 'react';
import { Clock } from 'lucide-react';

interface ScheduleSelectorProps {
  value: string;
  onChange: (cron: string) => void;
}

type ScheduleType = 'daily' | 'weekly' | 'monthly' | 'custom';

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function parseCron(cron: string): {
  type: ScheduleType;
  hour: number;
  minute: number;
  dayOfWeek: number;
  dayOfMonth: number;
} {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) {
    return { type: 'custom', hour: 3, minute: 0, dayOfWeek: 1, dayOfMonth: 1 };
  }
  const [min, hr, dom, , dow] = parts;
  const hour = hr === '*' ? 3 : parseInt(hr, 10);
  const minute = min === '*' ? 0 : parseInt(min, 10);

  if (dom === '*' && dow === '*') {
    return { type: 'daily', hour: isNaN(hour) ? 3 : hour, minute: isNaN(minute) ? 0 : minute, dayOfWeek: 1, dayOfMonth: 1 };
  }
  if (dom === '*' && dow !== '*') {
    return { type: 'weekly', hour: isNaN(hour) ? 3 : hour, minute: isNaN(minute) ? 0 : minute, dayOfWeek: parseInt(dow, 10) || 1, dayOfMonth: 1 };
  }
  if (dom !== '*' && dow === '*') {
    return { type: 'monthly', hour: isNaN(hour) ? 3 : hour, minute: isNaN(minute) ? 0 : minute, dayOfWeek: 1, dayOfMonth: parseInt(dom, 10) || 1 };
  }
  return { type: 'custom', hour: isNaN(hour) ? 3 : hour, minute: isNaN(minute) ? 0 : minute, dayOfWeek: 1, dayOfMonth: 1 };
}

function buildCron(type: ScheduleType, hour: number, minute: number, dayOfWeek: number, dayOfMonth: number): string {
  const h = hour.toString().padStart(2, '0');
  const m = minute.toString().padStart(2, '0');
  if (type === 'daily') return `${m} ${h} * * *`;
  if (type === 'weekly') return `${m} ${h} * * ${dayOfWeek}`;
  if (type === 'monthly') return `${m} ${h} ${dayOfMonth} * *`;
  return '';
}

export default function ScheduleSelector({ value, onChange }: ScheduleSelectorProps) {
  const parsed = parseCron(value || '0 3 * * *');
  const [type, setType] = useState<ScheduleType>(parsed.type);
  const [hour, setHour] = useState(parsed.hour);
  const [minute, setMinute] = useState(parsed.minute);
  const [dayOfWeek, setDayOfWeek] = useState(parsed.dayOfWeek);
  const [dayOfMonth, setDayOfMonth] = useState(parsed.dayOfMonth);
  const [customCron, setCustomCron] = useState(value || '0 3 * * *');

  useEffect(() => {
    if (type === 'custom') {
      onChange(customCron);
    } else {
      onChange(buildCron(type, hour, minute, dayOfWeek, dayOfMonth));
    }
  }, [type, hour, minute, dayOfWeek, dayOfMonth, customCron]);

  const hrPad = hour.toString().padStart(2, '0');
  const minPad = minute.toString().padStart(2, '0');

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm font-medium text-gray-700">
        <Clock size={14} />
        Schedule
      </div>

      <div className="flex flex-wrap gap-3">
        {/* Frequency */}
        <select
          value={type}
          onChange={(e) => setType(e.target.value as ScheduleType)}
          className="border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="daily">Every day</option>
          <option value="weekly">Every week on…</option>
          <option value="monthly">Every month on day…</option>
          <option value="custom">Custom (cron)</option>
        </select>

        {/* Day of week */}
        {type === 'weekly' && (
          <select
            value={dayOfWeek}
            onChange={(e) => setDayOfWeek(parseInt(e.target.value, 10))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {DAYS.map((day, i) => (
              <option key={day} value={i}>{day}</option>
            ))}
          </select>
        )}

        {/* Day of month */}
        {type === 'monthly' && (
          <select
            value={dayOfMonth}
            onChange={(e) => setDayOfMonth(parseInt(e.target.value, 10))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => (
              <option key={d} value={d}>{d}</option>
            ))}
          </select>
        )}

        {/* Time picker */}
        {type !== 'custom' && (
          <div className="flex items-center gap-1">
            <span className="text-sm text-gray-500">at</span>
            <select
              value={hrPad}
              onChange={(e) => setHour(parseInt(e.target.value, 10))}
              className="border border-gray-300 rounded-lg px-2 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              {Array.from({ length: 24 }, (_, i) => i).map((h) => (
                <option key={h} value={h.toString().padStart(2, '0')}>
                  {h.toString().padStart(2, '0')}
                </option>
              ))}
            </select>
            <span className="text-gray-500">:</span>
            <select
              value={minPad}
              onChange={(e) => setMinute(parseInt(e.target.value, 10))}
              className="border border-gray-300 rounded-lg px-2 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              {[0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55].map((m) => (
                <option key={m} value={m.toString().padStart(2, '0')}>
                  {m.toString().padStart(2, '0')}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* Custom cron input */}
      {type === 'custom' && (
        <div>
          <input
            type="text"
            value={customCron}
            onChange={(e) => setCustomCron(e.target.value)}
            placeholder="e.g. 0 3 * * *"
            className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <p className="text-xs text-gray-400 mt-1">Format: minute hour day-of-month month day-of-week</p>
        </div>
      )}

      {/* Preview */}
      {type !== 'custom' && (
        <p className="text-xs text-gray-400">
          Cron: <span className="font-mono">{buildCron(type, hour, minute, dayOfWeek, dayOfMonth)}</span>
        </p>
      )}
    </div>
  );
}
