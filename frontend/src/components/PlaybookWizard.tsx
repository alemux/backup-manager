import { useState, useEffect } from 'react';
import { X, Copy, Check, ChevronLeft, ChevronRight, CheckSquare, Square } from 'lucide-react';
import type { Playbook, Step } from '../api/recovery';

interface PlaybookWizardProps {
  playbook: Playbook;
  onClose: () => void;
}

function scenarioLabel(scenario: string): string {
  switch (scenario) {
    case 'full_server': return 'Full Server Recovery';
    case 'single_database': return 'Single Database Restore';
    case 'single_project': return 'Single Project Restore';
    case 'config_only': return 'Config Only Restore';
    case 'certificates': return 'Certificate Restore';
    default: return scenario;
  }
}

function storageKey(playbookId: number): string {
  return `playbook_progress_${playbookId}`;
}

function loadProgress(playbookId: number): Record<number, boolean> {
  try {
    const raw = localStorage.getItem(storageKey(playbookId));
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function saveProgress(playbookId: number, completed: Record<number, boolean>): void {
  try {
    localStorage.setItem(storageKey(playbookId), JSON.stringify(completed));
  } catch {
    // ignore storage errors
  }
}

interface CopyButtonProps {
  text: string;
}

function CopyButton({ text }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }).catch(() => {
      // fallback: select all text in a textarea
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <button
      onClick={handleCopy}
      className="inline-flex items-center gap-1 px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 text-gray-200 rounded transition-colors"
      title="Copy to clipboard"
    >
      {copied ? <Check size={12} className="text-green-400" /> : <Copy size={12} />}
      {copied ? 'Copied!' : 'Copy'}
    </button>
  );
}

interface StepCardProps {
  step: Step;
  isCompleted: boolean;
  onToggle: () => void;
}

function StepCard({ step, isCompleted, onToggle }: StepCardProps) {
  return (
    <div className={`rounded-lg border p-4 transition-colors ${isCompleted ? 'border-green-300 bg-green-50' : 'border-gray-200 bg-white'}`}>
      <div className="flex items-start gap-3">
        <button
          onClick={onToggle}
          className={`mt-0.5 flex-shrink-0 ${isCompleted ? 'text-green-600' : 'text-gray-400 hover:text-gray-600'}`}
          title={isCompleted ? 'Mark incomplete' : 'Mark complete'}
        >
          {isCompleted ? <CheckSquare size={20} /> : <Square size={20} />}
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <span className="text-xs font-medium text-gray-500 uppercase tracking-wide">
              Step {step.order}
            </span>
          </div>
          <h3 className={`font-semibold text-base mb-1 ${isCompleted ? 'text-green-800 line-through' : 'text-gray-900'}`}>
            {step.title}
          </h3>
          <p className="text-sm text-gray-600 mb-3">{step.description}</p>

          {step.command && (
            <div className="mb-3">
              <div className="flex items-center justify-between mb-1">
                <span className="text-xs font-medium text-gray-500 uppercase tracking-wide">Command</span>
                <CopyButton text={step.command} />
              </div>
              <pre className="bg-gray-900 text-green-300 text-xs rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono">
                {step.command}
              </pre>
            </div>
          )}

          {step.verify && (
            <div className="mb-3">
              <div className="flex items-center justify-between mb-1">
                <span className="text-xs font-medium text-blue-600 uppercase tracking-wide">Verify</span>
                <CopyButton text={step.verify} />
              </div>
              <pre className="bg-blue-900 text-blue-200 text-xs rounded p-3 overflow-x-auto whitespace-pre-wrap font-mono">
                {step.verify}
              </pre>
            </div>
          )}

          {step.notes && (
            <div className="bg-yellow-50 border border-yellow-200 rounded p-2">
              <span className="text-xs font-medium text-yellow-700 uppercase tracking-wide block mb-1">Note</span>
              <p className="text-xs text-yellow-800">{step.notes}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default function PlaybookWizard({ playbook, onClose }: PlaybookWizardProps) {
  const [completed, setCompleted] = useState<Record<number, boolean>>(() =>
    loadProgress(playbook.id)
  );
  const [currentStep, setCurrentStep] = useState(0);

  const steps = playbook.steps ?? [];
  const totalSteps = steps.length;
  const completedCount = Object.values(completed).filter(Boolean).length;
  const progressPercent = totalSteps > 0 ? Math.round((completedCount / totalSteps) * 100) : 0;

  useEffect(() => {
    saveProgress(playbook.id, completed);
  }, [completed, playbook.id]);

  function toggleStep(order: number) {
    setCompleted(prev => ({ ...prev, [order]: !prev[order] }));
  }

  function goNext() {
    setCurrentStep(s => Math.min(s + 1, totalSteps - 1));
  }

  function goPrev() {
    setCurrentStep(s => Math.max(s - 1, 0));
  }

  const currentStepData = steps[currentStep];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-white rounded-xl shadow-2xl w-full max-w-2xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-start justify-between p-5 border-b">
          <div className="flex-1 min-w-0 mr-3">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-xs font-medium text-blue-600 uppercase tracking-wide">
                {scenarioLabel(playbook.scenario)}
              </span>
            </div>
            <h2 className="text-lg font-bold text-gray-900 leading-tight">{playbook.title}</h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-gray-100 text-gray-500 flex-shrink-0"
          >
            <X size={20} />
          </button>
        </div>

        {/* Progress bar */}
        <div className="px-5 py-3 border-b bg-gray-50">
          <div className="flex items-center justify-between mb-1">
            <span className="text-sm font-medium text-gray-700">Progress</span>
            <span className="text-sm font-medium text-gray-700">
              {completedCount} / {totalSteps} steps ({progressPercent}%)
            </span>
          </div>
          <div className="w-full bg-gray-200 rounded-full h-2">
            <div
              className="bg-green-500 h-2 rounded-full transition-all duration-300"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        </div>

        {/* Step navigation tabs */}
        {totalSteps > 1 && (
          <div className="px-5 py-2 border-b bg-gray-50 flex gap-1 overflow-x-auto">
            {steps.map((step, idx) => (
              <button
                key={step.order}
                onClick={() => setCurrentStep(idx)}
                className={`flex-shrink-0 w-7 h-7 rounded-full text-xs font-medium transition-colors ${
                  idx === currentStep
                    ? 'bg-blue-600 text-white'
                    : completed[step.order]
                    ? 'bg-green-500 text-white'
                    : 'bg-gray-200 text-gray-600 hover:bg-gray-300'
                }`}
                title={step.title}
              >
                {step.order}
              </button>
            ))}
          </div>
        )}

        {/* Current step content */}
        <div className="flex-1 overflow-y-auto p-5">
          {currentStepData ? (
            <StepCard
              step={currentStepData}
              isCompleted={!!completed[currentStepData.order]}
              onToggle={() => toggleStep(currentStepData.order)}
            />
          ) : (
            <p className="text-gray-500 text-center py-8">No steps available.</p>
          )}
        </div>

        {/* Footer navigation */}
        <div className="flex items-center justify-between p-4 border-t bg-gray-50 rounded-b-xl">
          <button
            onClick={goPrev}
            disabled={currentStep === 0}
            className="inline-flex items-center gap-1 px-3 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <ChevronLeft size={16} />
            Previous
          </button>

          <span className="text-sm text-gray-500">
            Step {currentStep + 1} of {totalSteps}
          </span>

          <button
            onClick={goNext}
            disabled={currentStep === totalSteps - 1}
            className="inline-flex items-center gap-1 px-3 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Next
            <ChevronRight size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}
