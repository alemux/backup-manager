import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { snapshotsApi, type CalendarDay } from '../api/snapshots';

interface SnapshotCalendarProps {
  onDaySelect: (date: string | null) => void;
  selectedDate: string | null;
}

const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
];

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function getDayColor(day: CalendarDay | undefined): string {
  if (!day || day.count === 0) return '';
  if (day.failed_count > 0) return 'bg-red-100 border-red-200';
  return 'bg-green-100 border-green-200';
}

function getDotColor(day: CalendarDay | undefined): string {
  if (!day || day.count === 0) return 'bg-gray-300';
  if (day.failed_count > 0) return 'bg-red-500';
  return 'bg-green-500';
}

export default function SnapshotCalendar({ onDaySelect, selectedDate }: SnapshotCalendarProps) {
  const now = new Date();
  const [month, setMonth] = useState(now.getMonth() + 1); // 1-based
  const [year, setYear] = useState(now.getFullYear());

  const { data, isLoading } = useQuery({
    queryKey: ['snapshot-calendar', month, year],
    queryFn: () => snapshotsApi.getCalendar(month, year),
  });

  // Build a map date-string → CalendarDay
  const dayMap = new Map<string, CalendarDay>();
  if (data?.days) {
    for (const d of data.days) {
      dayMap.set(d.date, d);
    }
  }

  // Calendar grid
  const firstDay = new Date(year, month - 1, 1).getDay(); // 0=Sun
  const daysInMonth = new Date(year, month, 0).getDate();

  const cells: (number | null)[] = [];
  for (let i = 0; i < firstDay; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);
  // Pad to full weeks
  while (cells.length % 7 !== 0) cells.push(null);

  function prevMonth() {
    if (month === 1) { setMonth(12); setYear(y => y - 1); }
    else setMonth(m => m - 1);
  }

  function nextMonth() {
    if (month === 12) { setMonth(1); setYear(y => y + 1); }
    else setMonth(m => m + 1);
  }

  function goToday() {
    setMonth(now.getMonth() + 1);
    setYear(now.getFullYear());
  }

  function handleDayClick(day: number) {
    const dateStr = `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
    if (selectedDate === dateStr) {
      onDaySelect(null);
    } else {
      onDaySelect(dateStr);
    }
  }

  const isCurrentMonth = month === now.getMonth() + 1 && year === now.getFullYear();

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5 shadow-sm">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <button
          onClick={prevMonth}
          className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-500 hover:text-gray-700 transition-colors"
          aria-label="Previous month"
        >
          <ChevronLeft size={18} />
        </button>

        <div className="flex items-center gap-3">
          <h2 className="text-base font-semibold text-gray-900">
            {MONTH_NAMES[month - 1]} {year}
          </h2>
          {!isCurrentMonth && (
            <button
              onClick={goToday}
              className="text-xs text-blue-600 hover:text-blue-800 font-medium px-2 py-0.5 rounded border border-blue-200 hover:bg-blue-50 transition-colors"
            >
              Today
            </button>
          )}
        </div>

        <button
          onClick={nextMonth}
          className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-500 hover:text-gray-700 transition-colors"
          aria-label="Next month"
        >
          <ChevronRight size={18} />
        </button>
      </div>

      {/* Legend */}
      <div className="flex items-center gap-4 mb-3 text-xs text-gray-500">
        <span className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full bg-green-500 inline-block" />
          All OK
        </span>
        <span className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full bg-red-500 inline-block" />
          Some failed
        </span>
        <span className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full bg-gray-300 inline-block" />
          No backups
        </span>
      </div>

      {/* Loading */}
      {isLoading && (
        <div className="h-48 flex items-center justify-center text-sm text-gray-400">
          Loading calendar...
        </div>
      )}

      {!isLoading && (
        <div className="grid grid-cols-7 gap-1">
          {/* Day labels */}
          {DAY_LABELS.map(label => (
            <div key={label} className="text-center text-xs font-medium text-gray-400 py-1">
              {label}
            </div>
          ))}

          {/* Day cells */}
          {cells.map((day, idx) => {
            if (day === null) {
              return <div key={`empty-${idx}`} />;
            }

            const dateStr = `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
            const calDay = dayMap.get(dateStr);
            const isToday = day === now.getDate() && isCurrentMonth;
            const isSelected = selectedDate === dateStr;
            const hasData = calDay && calDay.count > 0;

            return (
              <button
                key={day}
                onClick={() => handleDayClick(day)}
                className={[
                  'relative flex flex-col items-center justify-center rounded-lg py-1.5 px-0.5 text-sm transition-all border',
                  isSelected
                    ? 'bg-blue-600 text-white border-blue-600 shadow-sm'
                    : hasData
                    ? `${getDayColor(calDay)} text-gray-800 hover:opacity-80 cursor-pointer`
                    : 'text-gray-600 border-transparent hover:bg-gray-50 cursor-pointer',
                  isToday && !isSelected ? 'font-bold ring-2 ring-blue-400 ring-offset-1' : '',
                ].join(' ')}
                title={
                  calDay
                    ? `${calDay.count} snapshots (${calDay.success_count} ok, ${calDay.failed_count} failed)`
                    : 'No snapshots'
                }
              >
                <span className="text-xs leading-none">{day}</span>
                {hasData && (
                  <span
                    className={[
                      'mt-0.5 w-1.5 h-1.5 rounded-full',
                      isSelected ? 'bg-white' : getDotColor(calDay),
                    ].join(' ')}
                  />
                )}
              </button>
            );
          })}
        </div>
      )}

      {/* Summary */}
      {data && (
        <div className="mt-3 pt-3 border-t border-gray-100 flex items-center justify-between text-xs text-gray-500">
          <span>
            {data.days.reduce((s, d) => s + d.count, 0)} snapshots this month
          </span>
          {selectedDate && (
            <button
              onClick={() => onDaySelect(null)}
              className="text-blue-600 hover:text-blue-800 font-medium"
            >
              Clear selection
            </button>
          )}
        </div>
      )}
    </div>
  );
}
