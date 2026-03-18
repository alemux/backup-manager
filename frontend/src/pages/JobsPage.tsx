import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, BriefcaseIcon, Loader2 } from 'lucide-react';
import { jobsApi, type Job } from '../api/jobs';
import JobCard from '../components/JobCard';
import CreateJobModal from '../components/CreateJobModal';
import RunHistory from '../components/RunHistory';

export default function JobsPage() {
  const [createOpen, setCreateOpen] = useState(false);

  const { data: jobs, isLoading, isError, error } = useQuery<Job[]>({
    queryKey: ['jobs'],
    queryFn: () => jobsApi.list(),
    refetchInterval: 30000,
  });

  return (
    <div className="p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Backup Jobs</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            Schedule and manage automated backup jobs.
          </p>
        </div>
        <button
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors shadow-sm"
        >
          <Plus size={16} />
          Create Job
        </button>
      </div>

      {/* Loading */}
      {isLoading && (
        <div className="flex items-center justify-center py-20">
          <Loader2 size={24} className="animate-spin text-blue-600" />
        </div>
      )}

      {/* Error */}
      {isError && (
        <div className="bg-red-50 border border-red-200 text-red-700 rounded-xl p-5 text-sm">
          Failed to load jobs: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      )}

      {/* Empty state */}
      {!isLoading && !isError && jobs?.length === 0 && (
        <div className="text-center py-20">
          <BriefcaseIcon size={48} className="text-gray-300 mx-auto mb-4" />
          <h3 className="text-base font-semibold text-gray-600 mb-1">No backup jobs yet</h3>
          <p className="text-sm text-gray-400 mb-6">
            Create your first backup job to start protecting your data.
          </p>
          <button
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors"
          >
            <Plus size={16} />
            Create Job
          </button>
        </div>
      )}

      {/* Jobs grid */}
      {!isLoading && !isError && jobs && jobs.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 mb-10">
          {jobs.map((job) => (
            <JobCard key={job.id} job={job} />
          ))}
        </div>
      )}

      {/* Run history */}
      {!isLoading && !isError && (
        <div className="bg-white border border-gray-200 rounded-xl p-5">
          <RunHistory />
        </div>
      )}

      {/* Create modal */}
      {createOpen && <CreateJobModal onClose={() => setCreateOpen(false)} />}
    </div>
  );
}
